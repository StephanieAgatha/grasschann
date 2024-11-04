# Grass Bot - WebSocket Proxy Manager

A high-performance WebSocket proxy manager written in Go, designed for handling multiple proxy connections with efficient resource management.

## Features

- Multiple proxy support per user ID
- Automated proxy rotation and distribution
- Real-time WebSocket communication
- IP geolocation checking
- Efficient connection pooling
- Colorized logging with timezone support

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
go.uber.org/zap
```

## Installation

```bash
# Clone the repository
git clone [repository-url]

# Change directory
cd grass-bot

# Install dependencies
go mod tidy

# Build the application
go build
```

## Configuration

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

## Usage

```bash
# Run the application
./grass-bot
```

## Technical Details

### Connection Configuration

```go
MaxConnsPerHost:      1000
ReadTimeout:          30s
WriteTimeout:         30s
MaxIdleConnDuration:  5m
MaxConnDuration:      10m
MaxConnWaitTimeout:   30s
RetryInterval:        20s
```

### WebSocket Protocol

- **Ping Interval**: 26 seconds
- **Authentication**: Custom device ID based on proxy
- **Device Type**: Desktop
- **Version**: 4.28.1

### Proxy Distribution

The system distributes proxies among users following these rules:
- Equal distribution of base proxies per user
- Additional proxies assigned to the first user if total proxies not evenly divisible
- Validation ensures user count doesn't exceed proxy count

### Error Handling

- Automatic reconnection on failure
- Configurable retry intervals
- Comprehensive error logging
- Connection timeout management

### Performance Optimizations

- FastHTTP client pool for reduced memory allocation
- Connection reuse with configurable timeouts
- Goroutines for concurrent connections
- Efficient proxy distribution algorithm

## Project Structure

```
├── main.go                  # Main application entry
├── proxy.txt               # Proxy list file
├── uid.txt                # User ID list file
└── README.md              # Documentation
```

## Core Components

1. **Bot**: Main controller managing WebSocket client and proxy checker
2. **DefaultWSClient**: WebSocket connection handler
3. **DefaultProxyChecker**: Proxy IP checking with FastHTTP
4. **FastHTTPClientPool**: Connection pool for optimized HTTP requests
5. **ProxyDistributor**: Handles proxy distribution among users

## Logging

The application uses Zap logger with:
- Colorized output
- Timezone support (Asia/Jakarta)
- Structured logging format
- Error tracking
- Connection status monitoring

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