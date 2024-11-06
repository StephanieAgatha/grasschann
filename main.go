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
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/itzngga/fake-useragent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
}

type FastHTTPClientPool struct {
	pool sync.Pool
}

type ProxyDistributor struct {
	userIDs []string
	proxies []string
	logger  *zap.Logger
}

type WSClient interface {
	Connect(ctx context.Context, proxy, userID string) error
}

type ProxyChecker interface {
	GetProxyIP(proxy string) (*IPInfo, error)
}

func (p *FastHTTPClientPool) Get() *fasthttp.Client {
	return p.pool.Get().(*fasthttp.Client)
}

func (p *FastHTTPClientPool) Put(c *fasthttp.Client) {
	p.pool.Put(c)
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

func NewDefaultProxyChecker(config Config) *DefaultProxyChecker {
	return &DefaultProxyChecker{
		config:     config,
		clientPool: NewFastHTTPClientPool(),
	}
}

func NewFastHTTPClientPool() *FastHTTPClientPool {
	return &FastHTTPClientPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &fasthttp.Client{
					MaxConnsPerHost:     1000,
					ReadTimeout:         30 * time.Second,
					WriteTimeout:        30 * time.Second,
					MaxIdleConnDuration: 5 * time.Minute,
					MaxConnDuration:     10 * time.Minute,
					MaxConnWaitTimeout:  30 * time.Second,
				}
			},
		},
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
	var proxyURL string
	if strings.HasPrefix(proxy, "socks5://") {
		proxyURL = proxy
	} else if strings.HasPrefix(proxy, "http://") {
		proxyURL = proxy
	} else {
		proxyURL = "socks5://" + proxy
	}

	client := pc.clientPool.Get()
	client.Dial = fasthttpproxy.FasthttpSocksDialer(proxyURL)
	client.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	defer pc.clientPool.Put(client)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(pc.config.IPCheckURL)
	req.Header.SetMethod("GET")

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := client.DoTimeout(req, resp, 30*time.Second); err != nil {
		return nil, fmt.Errorf("failed to perform GET request: %v", err)
	}

	var ipInfo IPInfo
	if err := json.Unmarshal(resp.Body(), &ipInfo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response body: %v", err)
	}

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
			_, message, err := c.ReadMessage()
			if err != nil {
				ws.logger.Error(fmt.Sprintf("Error reading message: %v", err))
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
						"version":     "4.28.2",
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
	deviceID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxy)).String()
	userAgent := browser.MacOSX()

	wsURL := fmt.Sprintf("wss://%s/", ws.config.WSSHost)
	ws.logger.Info(fmt.Sprintf("Connecting to %s", wsURL))

	headers := prepareHeaders(userAgent)

	dialer := websocket.Dialer{
		Proxy:            createProxyDialerWithFastHTTP(proxy),
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 30 * time.Second,
	}

	httpHeaders := fastHTTPHeadersToHTTP(headers)

	c, _, err := dialer.DialContext(ctx, wsURL, httpHeaders)
	if err != nil {
		return fmt.Errorf("error connecting to WebSocket: %v", err)
	}
	defer c.Close()

	ws.logger.Info("Connected to WebSocket")

	ipInfo, err := ws.proxyCheck.GetProxyIP(proxy)
	if err != nil {
		return fmt.Errorf("error getting proxy IP info: %v", err)
	}
	ws.logger.Info(fmt.Sprintf("Proxy location info - IP: %s, City: %s, Region: %s",
		ipInfo.IP, ipInfo.City, ipInfo.Region))

	go ws.sendPing(ctx, c, ipInfo.IP)
	ws.handleMessages(ctx, c, ipInfo, deviceID, userID)

	return nil
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

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

	done := make(chan struct{})

	go func() {
		for userID, userProxies := range proxyDistribution {
			for _, proxy := range userProxies {
				wg.Add(1)
				go func(proxy, userID string) {
					defer wg.Done()
					for {
						select {
						case <-ctx.Done():
							logger.Info("Shutting down connection",
								zap.String("userID", userID),
								zap.String("proxy", proxy))
							return
						default:
							if err := bot.wsClient.Connect(ctx, proxy, userID); err != nil {
								logger.Error("Connection error",
									zap.Error(err),
									zap.String("userID", userID),
									zap.String("proxy", proxy))
								select {
								case <-ctx.Done():
									return
								case <-time.After(config.RetryInterval):
									continue
								}
							}
						}
					}
				}(proxy, userID)
			}
		}
		wg.Wait()
		close(done)
	}()

	//handle shutdown
	select {
	case sig := <-signals:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
		//wait for graceful shutdown with timeout
		shutdownTimeout := time.NewTimer(30 * time.Second)
		select {
		case <-done:
			logger.Info("All connections closed successfully")
		case <-shutdownTimeout.C:
			logger.Warn("Shutdown timed out, forcing exit")
		}

		//final cleanup
		logger.Info("Cleaning up resources")
		time.Sleep(2 * time.Second) //give time for final cleanup

	case <-done:
		logger.Info("All connections finished naturally")
	}

	logger.Info("Program exiting")
}
