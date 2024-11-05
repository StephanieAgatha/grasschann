package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/itzngga/fake-useragent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultMaxConnsPerHost     = 1000
	defaultReadTimeout         = 30 * time.Second
	defaultWriteTimeout        = 30 * time.Second
	defaultMaxIdleConnDuration = 5 * time.Minute
	defaultMaxConnDuration     = 10 * time.Minute
	defaultMaxConnWaitTimeout  = 30 * time.Second
	defaultRetryAttempts       = 3
	defaultCacheDuration       = 5 * time.Minute
	defaultCacheCleanup        = 10 * time.Minute
	maxRetries                 = 10
	baseRetryDelay             = 5 * time.Second
	maxRetryDelay              = 30 * time.Second
)

type Config struct {
	ProxyURLTemplate string
	IPCheckURL       string
	WSSHost          string
	RetryInterval    time.Duration
}

type IPInfo struct {
	IP      string `json:"ip"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
}

type Bot struct {
	config     Config
	logger     *zap.Logger
	wsClient   WSClient
	proxyCheck ProxyChecker
}

type DefaultWSClient struct {
	config     Config
	logger     *zap.Logger
	proxyCheck ProxyChecker
}

type DefaultProxyChecker struct {
	config     Config
	clientPool *FastHTTPClientPool
	cache      *ProxyCache
}

type cacheEntry struct {
	data      *IPInfo
	timestamp time.Time
}

type ProxyCache struct {
	data  sync.Map
	ttl   time.Duration
	mutex sync.RWMutex
}

type FastHTTPClientPool struct {
	pool    sync.Pool
	metrics *Metrics
	mu      sync.RWMutex
}

type ProxyDistributor struct {
	userIDs []string
	proxies []string
	logger  *zap.Logger
}

type Metrics struct {
	ActiveConnections int32
	TotalRequests     int64
	FailedRequests    int64
	ResponseTimes     []time.Duration
}

type ProxyURL struct {
	Scheme   string
	Username string
	Password string
	Host     string
	Port     string
}

type WSClient interface {
	Connect(ctx context.Context, proxy, userID string) error
}

type ProxyChecker interface {
	GetProxyIP(proxy string) (*IPInfo, error)
}

func NewBot(config Config, logger *zap.Logger) *Bot {
	proxyChecker := NewDefaultProxyChecker(config)
	return &Bot{
		config:     config,
		logger:     logger,
		wsClient:   NewDefaultWSClient(config, logger, proxyChecker),
		proxyCheck: proxyChecker,
	}
}

func NewDefaultWSClient(config Config, logger *zap.Logger, proxyCheck ProxyChecker) *DefaultWSClient {
	return &DefaultWSClient{
		config:     config,
		logger:     logger,
		proxyCheck: proxyCheck,
	}
}

func NewProxyCache(ttl time.Duration) *ProxyCache {
	cache := &ProxyCache{
		ttl: ttl,
	}

	//start cleanup routine
	go cache.cleanup()

	return cache
}

func NewDefaultProxyChecker(config Config) *DefaultProxyChecker {
	return &DefaultProxyChecker{
		config:     config,
		clientPool: NewFastHTTPClientPool(),
		cache:      NewProxyCache(defaultCacheDuration),
	}
}

func NewFastHTTPClientPool() *FastHTTPClientPool {
	return &FastHTTPClientPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &fasthttp.Client{
					MaxConnsPerHost:               defaultMaxConnsPerHost,
					ReadTimeout:                   defaultReadTimeout,
					WriteTimeout:                  defaultWriteTimeout,
					MaxIdleConnDuration:           defaultMaxIdleConnDuration,
					MaxConnDuration:               defaultMaxConnDuration,
					MaxConnWaitTimeout:            defaultMaxConnWaitTimeout,
					NoDefaultUserAgentHeader:      true,             //reduce memory alloc
					DisableHeaderNamesNormalizing: true,             //optimize perform
					DisablePathNormalizing:        true,             //optimize perform
					MaxResponseBodySize:           30 * 1024 * 1024, //30MB max response
					MaxIdemponentCallAttempts:     defaultRetryAttempts,
				}
			},
		},
		metrics: &Metrics{
			ResponseTimes: make([]time.Duration, 0, 1000), //pre-allocate slice
		},
	}
}

// get gets value from cache
func (c *ProxyCache) Get(key string) (*IPInfo, bool) {
	value, exists := c.data.Load(key)
	if !exists {
		return nil, false
	}

	entry := value.(*cacheEntry)

	//check if entry is expired
	if time.Since(entry.timestamp) > c.ttl {
		c.data.Delete(key)
		return nil, false
	}

	return entry.data, true
}

// set sets value in cache
func (c *ProxyCache) Set(key string, value *IPInfo) {
	c.data.Store(key, &cacheEntry{
		data:      value,
		timestamp: time.Now(),
	})
}

// cleanup removes expired entries
func (c *ProxyCache) cleanup() {
	ticker := time.NewTicker(defaultCacheCleanup)
	defer ticker.Stop()

	for range ticker.C {
		c.data.Range(func(key, value interface{}) bool {
			entry := value.(*cacheEntry)
			if time.Since(entry.timestamp) > c.ttl {
				c.data.Delete(key)
			}
			return true
		})
	}
}

func (p *FastHTTPClientPool) Get() *fasthttp.Client {
	atomic.AddInt32(&p.metrics.ActiveConnections, 1)
	client := p.pool.Get().(*fasthttp.Client)
	return client
}

func (p *FastHTTPClientPool) Put(c *fasthttp.Client) {
	c.Dial = nil
	c.TLSConfig = nil

	atomic.AddInt32(&p.metrics.ActiveConnections, -1)
	p.pool.Put(c)
}

func (p *FastHTTPClientPool) AddResponseTime(duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.metrics.ResponseTimes = append(p.metrics.ResponseTimes, duration)

	//keep only last 1000 response times
	if len(p.metrics.ResponseTimes) > 1000 {
		p.metrics.ResponseTimes = p.metrics.ResponseTimes[1:]
	}
}

func fastHTTPHeadersToHTTP(fHeaders *fasthttp.RequestHeader) http.Header {
	httpHeaders := make(http.Header)
	fHeaders.VisitAll(func(key, value []byte) {
		httpHeaders.Set(string(key), string(value))
	})
	return httpHeaders
}

func initLogger() *zap.Logger {
	jakartaLocation, _ := time.LoadLocation("Asia/Jakarta")

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:       "T",
		LevelKey:      "L",
		NameKey:       "N",
		CallerKey:     "C",
		MessageKey:    "M",
		StacktraceKey: "S",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel:   zapcore.CapitalColorLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.In(jakartaLocation).Format("02/01/2006-15:04:05"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "console",
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	core := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), config.Level)
	return zap.New(core)
}

func (pc *DefaultProxyChecker) GetProxyIP(proxy string) (*IPInfo, error) {
	// Check cache first
	if cached, exists := pc.cache.Get(proxy); exists {
		return cached, nil
	}

	start := time.Now()
	proxyURL := pc.normalizeProxyURL(proxy)

	client := pc.clientPool.Get()
	defer func() {
		pc.clientPool.Put(client)
		pc.clientPool.AddResponseTime(time.Since(start))
	}()

	client.Dial = fasthttpproxy.FasthttpSocksDialer(proxyURL)
	client.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(pc.config.IPCheckURL)
	req.Header.SetMethod(fasthttp.MethodGet)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	var err error
	for i := 0; i < defaultRetryAttempts; i++ {
		err = client.DoTimeout(req, resp, defaultReadTimeout)
		if err == nil {
			break
		}

		if i < defaultRetryAttempts-1 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}

	if err != nil {
		atomic.AddInt64(&pc.clientPool.metrics.FailedRequests, 1)
		return nil, fmt.Errorf("request failed after %d attempts: %w", defaultRetryAttempts, err)
	}

	atomic.AddInt64(&pc.clientPool.metrics.TotalRequests, 1)

	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	var ipInfo IPInfo
	if err := json.Unmarshal(resp.Body(), &ipInfo); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	//set cache
	pc.cache.Set(proxy, &ipInfo)

	return &ipInfo, nil
}

func prepareHeaders(userAgent string) *fasthttp.RequestHeader {
	headers := &fasthttp.RequestHeader{}
	headers.SetUserAgent(userAgent)
	headers.Set("pragma", "no-cache")
	headers.Set("Accept-Language", "uk-UA,uk;q=0.9,en-US;q=0.8,en;q=0.7")
	headers.Set("Cache-Control", "no-cache")
	return headers
}

func createProxyDialerWithFastHTTP(proxyAddr string) func(*http.Request) (*url.URL, error) {
	return func(*http.Request) (*url.URL, error) {
		if strings.HasPrefix(proxyAddr, "socks5://") {
			return url.Parse(proxyAddr)
		} else if strings.HasPrefix(proxyAddr, "http://") {
			return url.Parse(proxyAddr)
		}
		return url.Parse("socks5://" + proxyAddr)
	}
}

func (ws *DefaultWSClient) sendPing(ctx context.Context, c *websocket.Conn, proxyIP string) {
	ticker := time.NewTicker(26 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			message := map[string]interface{}{
				"id":      uuid.New().String(),
				"version": "1.0.0",
				"action":  "PING",
				"data":    map[string]interface{}{},
			}
			if err := c.WriteJSON(message); err != nil {
				ws.logger.Error(fmt.Sprintf("Error sending ping: %v", err))
				return
			}
			ws.logger.Info(fmt.Sprintf("Sent ping - IP: %s, Message: %v", proxyIP, message))
		case <-ctx.Done():
			return
		}
	}
}

func (ws *DefaultWSClient) handleMessages(ctx context.Context, c *websocket.Conn, ipInfo *IPInfo, deviceID, userID string) {
	userAgent := browser.MacOSX()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			//read deadline
			if err := c.SetReadDeadline(time.Now().Add(35 * time.Second)); err != nil {
				ws.logger.Error("Failed to set read deadline", zap.Error(err))
				return
			}

			_, message, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					ws.logger.Error(fmt.Sprintf("Error reading message: %v", err))
				}
				return
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				ws.logger.Error(fmt.Sprintf("Error unmarshalling message: %v", err))
				continue
			}

			ws.logger.Info(fmt.Sprintf("Received message from IP %s: %v", ipInfo.IP, msg))

			switch msg["action"].(string) {
			case "AUTH":
				authResponse := map[string]interface{}{
					"id":            msg["id"].(string),
					"origin_action": "AUTH",
					"result": map[string]interface{}{
						"browser_id":  deviceID,
						"user_id":     userID,
						"user_agent":  userAgent,
						"timestamp":   time.Now().Unix(),
						"device_type": "desktop",
						"version":     "4.28.1",
					},
				}
				if err := c.WriteJSON(authResponse); err != nil {
					ws.logger.Error(fmt.Sprintf("Error sending auth response: %v", err))
					return
				}
				ws.logger.Info(fmt.Sprintf("Sent auth response - IP: %s, Response: %v", ipInfo.IP, authResponse))
			case "PONG":
				pongResponse := map[string]interface{}{
					"id":            msg["id"].(string),
					"origin_action": "PONG",
				}
				if err := c.WriteJSON(pongResponse); err != nil {
					ws.logger.Error(fmt.Sprintf("Error sending pong response: %v", err))
					return
				}
				ws.logger.Info(fmt.Sprintf("Sent pong response - IP: %s, Response: %v", ipInfo.IP, pongResponse))
			}
		}
	}
}

func (ws *DefaultWSClient) Connect(ctx context.Context, proxy, userID string) error {
	retryCount := 0
	for {
		err := ws.attemptConnection(ctx, proxy, userID)
		if err != nil {
			retryCount++
			if retryCount > maxRetries {
				ws.logger.Error("Max retries reached",
					zap.String("userID", userID),
					zap.String("proxy", proxy),
					zap.Error(err))
				retryCount = 0
				time.Sleep(maxRetryDelay)
				continue
			}

			//calculate delay with exponential backoff
			delay := baseRetryDelay * time.Duration(1<<uint(retryCount))
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}

			ws.logger.Warn("Connection failed, retrying",
				zap.String("userID", userID),
				zap.String("proxy", proxy),
				zap.Duration("delay", delay),
				zap.Int("retry", retryCount),
				zap.Error(err))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		// Reset retry count on successful connection
		retryCount = 0
	}
}

func (ws *DefaultWSClient) attemptConnection(ctx context.Context, proxy, userID string) error {
	deviceID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxy)).String()
	userAgent := browser.MacOSX()

	ipInfo, err := ws.proxyCheck.GetProxyIP(proxy)
	if err != nil {
		return fmt.Errorf("proxy check failed: %w", err)
	}

	wsURL := fmt.Sprintf("wss://%s/", ws.config.WSSHost)
	ws.logger.Info(fmt.Sprintf("Connecting to %s using IP %s", wsURL, ipInfo.IP))

	headers := prepareHeaders(userAgent)
	httpHeaders := fastHTTPHeadersToHTTP(headers)

	dialer := websocket.Dialer{
		Proxy:            createProxyDialerWithFastHTTP(proxy),
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: defaultReadTimeout,
		WriteBufferSize:  4096,
		ReadBufferSize:   4096,
	}

	c, _, err := dialer.DialContext(ctx, wsURL, httpHeaders)
	if err != nil {
		return fmt.Errorf("websocket connection failed: %w", err)
	}

	defer func() {
		c.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.Close()
	}()

	c.EnableWriteCompression(true)
	c.SetReadLimit(32 << 20)
	c.SetReadDeadline(time.Now().Add(35 * time.Second))

	errChan := make(chan error, 2)
	done := make(chan struct{})
	defer close(done)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ws.logger.Error("Panic in ping handler", zap.Any("error", r))
				errChan <- fmt.Errorf("ping handler panic: %v", r)
			}
		}()
		ws.sendPing(ctx, c, ipInfo.IP)
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ws.logger.Error("Panic in message handler", zap.Any("error", r))
				errChan <- fmt.Errorf("message handler panic: %v", r)
			}
		}()
		ws.handleMessages(ctx, c, ipInfo, deviceID, userID)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (pd *ProxyDistributor) Validate() error {
	if len(pd.userIDs) == 0 || len(pd.proxies) == 0 {
		return fmt.Errorf("no user IDs or proxies found")
	}

	if len(pd.userIDs) > len(pd.proxies) {
		return fmt.Errorf("number of user IDs (%d) cannot be greater than number of proxies (%d)",
			len(pd.userIDs), len(pd.proxies))
	}

	return nil
}

func (pd *ProxyDistributor) DistributeProxies() map[string][]string {
	distribution := make(map[string][]string)
	baseProxiesPerUser := len(pd.proxies) / len(pd.userIDs)
	remainingProxies := len(pd.proxies) % len(pd.userIDs)

	currentIndex := 0
	for i, userID := range pd.userIDs {
		proxiesForThisUser := baseProxiesPerUser
		if i == 0 {
			proxiesForThisUser += remainingProxies
		}

		distribution[userID] = pd.proxies[currentIndex : currentIndex+proxiesForThisUser]
		currentIndex += proxiesForThisUser

		pd.logger.Info("Distributed proxies for user",
			zap.String("userID", userID),
			zap.Int("proxyCount", len(distribution[userID])))
	}

	return distribution
}

func NewProxyDistributor(userIDs, proxies []string, logger *zap.Logger) *ProxyDistributor {
	return &ProxyDistributor{
		userIDs: userIDs,
		proxies: proxies,
		logger:  logger,
	}
}

// normalizeProxyURL normalizes and validates proxy URL
func (pc *DefaultProxyChecker) normalizeProxyURL(proxy string) string {
	// Remove whitespace
	proxy = strings.TrimSpace(proxy)

	// If already has scheme, return as is
	if strings.HasPrefix(proxy, "socks5://") || strings.HasPrefix(proxy, "http://") {
		return proxy
	}

	var proxyURL ProxyURL

	// Split credentials and host
	parts := strings.Split(proxy, "@")
	if len(parts) == 2 {
		// Has credentials
		creds := strings.Split(parts[0], ":")
		if len(creds) == 2 {
			proxyURL.Username = creds[0]
			proxyURL.Password = creds[1]
		}
		hostPort := strings.Split(parts[1], ":")
		if len(hostPort) == 2 {
			proxyURL.Host = hostPort[0]
			proxyURL.Port = hostPort[1]
		}
	} else {
		// No credentials
		hostPort := strings.Split(proxy, ":")
		if len(hostPort) == 2 {
			proxyURL.Host = hostPort[0]
			proxyURL.Port = hostPort[1]
		}
	}

	var builder strings.Builder
	builder.WriteString("socks5://")

	if proxyURL.Username != "" {
		builder.WriteString(url.QueryEscape(proxyURL.Username))
		if proxyURL.Password != "" {
			builder.WriteString(":")
			builder.WriteString(url.QueryEscape(proxyURL.Password))
		}
		builder.WriteString("@")
	}

	builder.WriteString(proxyURL.Host)
	if proxyURL.Port != "" {
		builder.WriteString(":")
		builder.WriteString(proxyURL.Port)
	}

	return builder.String()
}

func readLines(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

func main() {
	config := Config{
		ProxyURLTemplate: "http://%s",
		IPCheckURL:       "https://ipinfo.io/json",
		WSSHost:          "proxy.wynd.network:4444",
		RetryInterval:    20 * time.Second,
	}

	logger := initLogger()
	defer logger.Sync()

	proxies, err := readLines("proxy.txt")
	if err != nil {
		logger.Fatal("Error reading proxies", zap.Error(err))
	}

	userIDs, err := readLines("uid.txt")
	if err != nil {
		logger.Fatal("Error reading user IDs", zap.Error(err))
	}

	distributor := NewProxyDistributor(userIDs, proxies, logger)
	if err := distributor.Validate(); err != nil {
		logger.Fatal("Proxy distribution validation failed", zap.Error(err))
	}

	proxyDistribution := distributor.DistributeProxies()

	bot := NewBot(config, logger)
	var wg sync.WaitGroup

	for userID, userProxies := range proxyDistribution {
		for _, proxy := range userProxies {
			wg.Add(1)
			go func(proxy, userID string) {
				defer wg.Done()
				for {
					if err := bot.wsClient.Connect(context.Background(), proxy, userID); err != nil {
						logger.Error("Connection error",
							zap.Error(err),
							zap.String("userID", userID),
							zap.String("proxy", proxy))
						time.Sleep(config.RetryInterval)
						continue
					}
				}
			}(proxy, userID)
		}
	}

	wg.Wait()
}
