package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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

type WSClient interface {
	Connect(ctx context.Context, proxy, userID string) error
}

type ProxyChecker interface {
	GetProxyIP(proxy string) (*IPInfo, error)
}

type DefaultWSClient struct {
	config     Config
	logger     *zap.Logger
	proxyCheck ProxyChecker
}

type DefaultProxyChecker struct {
	config Config
	client *http.Client
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
		config: config,
		client: &http.Client{},
	}
}

func (pc *DefaultProxyChecker) GetProxyIP(proxy string) (*IPInfo, error) {
	var proxyURL *url.URL
	var err error

	if strings.HasPrefix(proxy, "socks5://") {
		proxyURL, err = url.Parse(proxy)
	} else if strings.HasPrefix(proxy, "http://") {
		proxyURL, err = url.Parse(proxy)
	} else {
		proxyURL, err = url.Parse("socks5://" + proxy)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse proxy URL: %v", err)
	}

	//transport with proxy
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	request, err := http.NewRequest("GET", pc.config.IPCheckURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to form GET request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to perform GET request: %v", err)
	}
	defer response.Body.Close()

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %v", err)
	}

	var ipInfo IPInfo
	if err := json.Unmarshal(responseBody, &ipInfo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response body: %v", err)
	}

	return &ipInfo, nil
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
	userAgent := browser.Random()

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
						//"device_type":  "extension",
						//"extension_id": "lkbnfiajjmbhnfledhphioinpickokdi",
						"version": "4.28.1",
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
	var proxyURL *url.URL
	var err error

	if strings.HasPrefix(proxy, "socks5://") {
		proxyURL, err = url.Parse(proxy)
	} else if strings.HasPrefix(proxy, "http://") {
		proxyURL, err = url.Parse(proxy)
	} else {
		proxyURL, err = url.Parse("socks5://" + proxy)
	}

	if err != nil {
		return fmt.Errorf("error parsing proxy URL: %v", err)
	}

	deviceID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(proxy)).String()
	userAgent := browser.Random()

	u := url.URL{Scheme: "wss", Host: ws.config.WSSHost, Path: "/"}
	ws.logger.Info(fmt.Sprintf("Connecting to %s", u.String()))

	dialer := websocket.Dialer{
		Proxy:            http.ProxyURL(proxyURL),
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 30 * time.Second,
	}

	headers := http.Header{}
	headers.Set("User-Agent", userAgent)
	headers.Set("pragma", "no-cache")
	//headers.Set("Origin", "chrome-extension://lkbnfiajjmbhnfledhphioinpickokdi")
	headers.Set("Accept-Language", "uk-UA,uk;q=0.9,en-US;q=0.8,en;q=0.7")
	headers.Set("Cache-Control", "no-cache")

	c, _, err := dialer.DialContext(ctx, u.String(), headers)
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

type ProxyDistributor struct {
	userIDs []string
	proxies []string
	logger  *zap.Logger
}

func NewProxyDistributor(userIDs, proxies []string, logger *zap.Logger) *ProxyDistributor {
	return &ProxyDistributor{
		userIDs: userIDs,
		proxies: proxies,
		logger:  logger,
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

func normalizeProxy(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if strings.HasPrefix(proxy, "http://") {
		return strings.TrimPrefix(proxy, "http://")
	}
	if strings.HasPrefix(proxy, "socks5://") {
		return strings.TrimPrefix(proxy, "socks5://")
	}
	return proxy
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
