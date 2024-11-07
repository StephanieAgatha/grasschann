# GrassChann

A high-performance proxy sender written in Go, designed for managing multiple proxy connections to Grass ecosystem through WebSocket. Experience blazing-fast connectivity with optimized connection pooling, smart proxy distribution, and rock-solid stability. Built with FastHTTP for lightning-quick proxy verification and Gorilla WebSocket for unbreakable server connections.

## ⚡ Key Features

- High throughput proxy connection management
- Smart proxy rotation and auto-distribution
- Real-time connection monitoring
- Proxy health verification via ipinfo.io
- Connection pooling with FastHTTP
- Beautiful logging with Charmbracelet's Log & Lipgloss
- Memory-efficient resource management
- Graceful shutdown handling

## 🔥 Performance Highlights
- Lightning-fast connection establishment
- Efficient proxy health checking
- Optimized memory usage
- Smart resource management
- Auto-reconnection on failures
- Concurrent proxy handling

## 🛡️ Graceful Shutdown
The application implements graceful shutdown handling:
- Captures SIGTERM and SIGINT signals
- Gracefully closes all active connections
- Clean resource management
- Configurable shutdown timeout
- Detailed shutdown logging

## Prerequisites

```bash
go 1.19 or higher
```

## Dependencies

```go
github.com/google/uuid
github.com/gorilla/websocket
github.com/itzngga/fake-useragent
github.com/valyala/fasthttp
github.com/valyala/fasthttp/fasthttpproxy
github.com/charmbracelet/log
```

## Installation

```bash
# Clone the repository
git clone https://github.com/StephanieAgatha/grasschann.git

# Change directory
cd grasschann

# Install dependencies
go mod tidy

# Run the application
go run main.go
```

## Configuration

### Files Setup
Create the following files in your project directory:

1. `proxy.txt` - List of proxies, one per line:
```
socks5://user:pass@host:port
http://user:pass@host:port
user:pass@host:port
```

2. `uid.txt` - List of user IDs, one per line:
```
user-id-1
user-id-2
```

## Technical Configuration

### Connection Parameters
```go
MaxConnsPerHost:      1000
ReadTimeout:          30s
WriteTimeout:         30s
MaxIdleConnDuration:  5m
MaxConnDuration:      10m
MaxConnWaitTimeout:   30s
PingInterval:         26s
RetryInterval:        20s
```

## Key Features

### 1. FastHTTP Integration
- Client pooling for resource efficiency
- Memory-optimized HTTP requests
- Connection reuse

### 2. Proxy Management
- Automatic proxy distribution
- Health checking via ipinfo.io
- Supports SOCKS5 and HTTP proxies

### 3. WebSocket Features
- Real-time communication
- Ping/Pong mechanism
- Auto reconnection
- Error recovery

### 4. Performance Features
- Connection pooling
- Memory optimization
- Resource cleanup
- Goroutine management

### 5. Logging
- Beautiful and informative logging with Charmbracelet Log
- Custom styled output with Lipgloss
- Asia/Jakarta timezone support
- Comprehensive error tracking
- Pretty-formatted JSON output

## Project Structure
```
├── main.go             # Main application entry
├── proxy.txt           # Proxy list file
├── uid.txt             # User ID list file
└── README.md           # Documentation
```

## Core Components

1. **Bot**: Main controller for WebSocket client and proxy checker
2. **DefaultWSClient**: WebSocket connection handler
3. **DefaultProxyChecker**: Proxy IP checking with FastHTTP
4. **FastHTTPClientPool**: Connection pool for optimized HTTP requests
5. **ProxyDistributor**: Handles proxy distribution among users

## Author

- Stephanie Agatha
- GitHub: [@StephanieAgatha](https://github.com/StephanieAgatha)

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

1. Fork the project
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Disclaimer

This tool is for educational purposes only. Make sure to comply with the terms of service of any services you interact with.