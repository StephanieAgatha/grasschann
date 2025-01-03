package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	browser "github.com/itzngga/fake-useragent"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
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
	logger     *log.Logger
	wsClient   WSClient
	proxyCheck ProxyChecker
}

type DefaultWSClient struct {
	config     Config
	logger     *log.Logger
	proxyCheck ProxyChecker
	writeMu    sync.Mutex
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
	logger  *log.Logger
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

func NewBot(config Config, logger *log.Logger) *Bot {
	proxyChecker := NewDefaultProxyChecker(config)
	return &Bot{
		config:     config,
		logger:     logger,
		wsClient:   NewDefaultWSClient(config, logger, proxyChecker),
		proxyCheck: proxyChecker,
	}
}

func NewDefaultWSClient(config Config, logger *log.Logger, proxyCheck ProxyChecker) *DefaultWSClient {
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

func (ws *DefaultWSClient) writeJSON(c *websocket.Conn, v interface{}) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return c.WriteJSON(v)
}

func fastHTTPHeadersToHTTP(fHeaders *fasthttp.RequestHeader) http.Header {
	httpHeaders := make(http.Header)
	fHeaders.VisitAll(func(key, value []byte) {
		httpHeaders.Set(string(key), string(value))
	})
	return httpHeaders
}

func initLogger() *log.Logger {
	// new log using charm and 💄(lipgloss) haha
	logger := log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
		Level:           log.InfoLevel,
		Prefix:          "Grass 🌱",
	})

	// get default styles and customize them
	styles := log.DefaultStyles()

	// customize error level appearance
	styles.Levels[log.ErrorLevel] = lipgloss.NewStyle().
		SetString("ERROR").
		Padding(0, 1, 0, 1).
		Foreground(lipgloss.Color("204")) // red

	//make error messages and values stand out
	styles.Keys["error"] = lipgloss.NewStyle().
		Foreground(lipgloss.Color("204")) // red color for error keys
	styles.Values["error"] = lipgloss.NewStyle()

	// change all regular keys to cyan
	styles.Key = lipgloss.NewStyle().
		Foreground(lipgloss.Color("51")) // bright cyan color for all keys

	// set customized styles
	logger.SetStyles(styles)

	// set env timezone to asia/jkt
	os.Setenv("TZ", "Asia/Jakarta")

	return logger
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
	// Immediately start sending pings as we're only called after HTTP_REQUEST is processed
	message := map[string]interface{}{
		"id":      uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxyIP)).String(),
		"version": "1.0.0",
		"action":  "PING",
		"data":    map[string]interface{}{},
	}

	if err := ws.writeJSON(c, message); err != nil {
		ws.logger.Error("error sending initial ping", "error", err)
		return
	}
	ws.logger.Info("sending ping", "ip", proxyIP, "message", message)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			message := map[string]interface{}{
				"id":      uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxyIP)).String(),
				"version": "1.0.0",
				"action":  "PING",
				"data":    map[string]interface{}{},
			}
			if err := ws.writeJSON(c, message); err != nil {
				ws.logger.Error("error sending ping", "error", err)
				return
			}
			ws.logger.Info("sending ping", "ip", proxyIP, "message", message)
		}
	}
}

func (ws *DefaultWSClient) handleMessages(ctx context.Context, c *websocket.Conn, ipInfo *IPInfo, deviceID, userID, userAgent string) {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	var pingStarted bool

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := c.ReadMessage()
			if err != nil {
				ws.logger.Error("error reading message", "error", err)
				return
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				ws.logger.Error("error unmarshalling message", "error", err)
				continue
			}

			ws.logger.Info("receive", "message", msg)

			action, ok := msg["action"].(string)
			if !ok {
				ws.logger.Error("invalid action type")
				continue
			}

			messageID, ok := msg["id"].(string)
			if !ok {
				ws.logger.Error("invalid message ID type")
				continue
			}

			switch action {
			case "AUTH":
				authResponse := map[string]interface{}{
					"id":            messageID,
					"origin_action": "AUTH",
					"result": map[string]interface{}{
						"browser_id":   deviceID,
						"user_id":      userID,
						"user_agent":   userAgent,
						"timestamp":    time.Now().Unix(),
						"device_type":  "extension",
						"version":      "4.26.2",
						"extension_id": "lkbnfiajjmbhnfledhphioinpickokdi",
					},
				}
				if err := ws.writeJSON(c, authResponse); err != nil {
					ws.logger.Error("error sending auth response", "error", err)
					return
				}
				ws.logger.Info("sending", "message", authResponse)

			case "HTTP_REQUEST":
				data, ok := msg["data"].(map[string]interface{})
				if !ok {
					ws.logger.Error("invalid data type in HTTP_REQUEST")
					continue
				}

				url, ok := data["url"].(string)
				if !ok {
					ws.logger.Error("invalid URL type in HTTP_REQUEST")
					continue
				}

				req, err := http.NewRequest("GET", url, nil)
				if err != nil {
					ws.logger.Error("error creating request", "error", err)
					continue
				}

				req.Header.Set("Accept", "*/*")
				req.Header.Set("Host", "api.getgrass.io")
				req.Header.Set("User-Agent", "wynd.network/3.0.1")

				resp, err := httpClient.Do(req)
				if err != nil {
					ws.logger.Error("error making request", "error", err)
					continue
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					ws.logger.Error("error reading response body", "error", err)
					continue
				}

				headers := make(map[string]string)
				for key, values := range resp.Header {
					headers[key] = values[0]
				}

				response := map[string]interface{}{
					"id":            messageID,
					"origin_action": "HTTP_REQUEST",
					"result": map[string]interface{}{
						"url":         url,
						"status":      resp.StatusCode,
						"status_text": "",
						"headers":     headers,
						"body":        base64.StdEncoding.EncodeToString(body),
					},
				}

				if err := ws.writeJSON(c, response); err != nil {
					ws.logger.Error("error sending HTTP response", "error", err)
					return
				}
				ws.logger.Info("sending", "message", response)

				if !pingStarted {
					pingStarted = true
					time.Sleep(time.Second)
					go ws.sendPing(ctx, c, ipInfo.IP)
				}

			case "PONG":
				pongResponse := map[string]interface{}{
					"id":            messageID,
					"origin_action": "PONG",
				}
				if err := ws.writeJSON(c, pongResponse); err != nil {
					ws.logger.Error("error sending pong response", "error", err)
					return
				}
				ws.logger.Info("sending", "message", pongResponse)
			}
		}
	}
}

func (ws *DefaultWSClient) Connect(ctx context.Context, proxy, userID string) error {
	deviceID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxy)).String()
	userAgent := browser.MacOSX()

	wsURL := fmt.Sprintf("wss://%s/", ws.config.WSSHost)
	ws.logger.Info("connecting to websocket", "url", wsURL)

	headers := &fasthttp.RequestHeader{}
	headers.Set("User-Agent", userAgent)
	headers.Set("pragma", "no-cache")
	headers.Set("Origin", "chrome-extension://lkbnfiajjmbhnfledhphioinpickokdi")
	headers.Set("Accept-Language", "uk-UA,uk;q=0.9,en-US;q=0.8,en;q=0.7")
	headers.Set("Cache-Control", "no-cache")

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

	ws.logger.Info("connected to websocket")

	ipInfo, err := ws.proxyCheck.GetProxyIP(proxy)
	if err != nil {
		return fmt.Errorf("error getting proxy IP info: %v", err)
	}
	ws.logger.Info("proxy location info",
		"ip", ipInfo.IP,
		"city", ipInfo.City,
		"region", ipInfo.Region)

	ws.handleMessages(ctx, c, ipInfo, deviceID, userID, userAgent)
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

		pd.logger.Info("distributed proxies for user",
			"userID", userID,
			"proxyCount", len(distribution[userID]))
	}

	return distribution
}

func NewProxyDistributor(userIDs, proxies []string, logger *log.Logger) *ProxyDistributor {
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
		WSSHost:          "proxy2.wynd.network:4650",
		RetryInterval:    20 * time.Second,
	}

	logger := initLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	proxies, err := readLines("proxy.txt")
	if err != nil {
		logger.Fatal("error reading proxies", "error", err)
	}

	userIDs, err := readLines("uid.txt")
	if err != nil {
		logger.Fatal("error reading user IDs", "error", err)
	}

	distributor := NewProxyDistributor(userIDs, proxies, logger)
	if err := distributor.Validate(); err != nil {
		logger.Fatal("proxy distribution validation failed", "error", err)
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
							logger.Info("shutting down connection",
								"userID", userID,
								"proxy", proxy)
							return
						default:
							if err := bot.wsClient.Connect(ctx, proxy, userID); err != nil {
								logger.Error("connection error",
									"error", err,
									"userID", userID,
									"proxy", proxy)
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

	select {
	case sig := <-signals:
		logger.Info("received shutdown signal", "signal", sig.String())
		cancel()
		shutdownTimeout := time.NewTimer(30 * time.Second)
		select {
		case <-done:
			logger.Info("all connections closed successfully")
		case <-shutdownTimeout.C:
			logger.Warn("shutdown timed out, forcing exit")
		}

		logger.Info("cleaning up resources")
		time.Sleep(2 * time.Second)

	case <-done:
		logger.Info("all connections finished naturally")
	}

	logger.Info("program exiting")
}
