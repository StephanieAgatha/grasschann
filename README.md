# Grass Bot

A high-performance WebSocket proxy manager written in Go, designed for handling multiple proxy connections with efficient resource management and automatic reconnection capabilities.

## Features

- Multiple proxy support per user ID with automatic distribution
- IP geolocation checking
- Real-time WebSocket communication with ping/pong mechanism
- Connection pooling with FastHTTP for optimal performance
- Exponential backoff retry mechanism
- Active connection monitoring
- Colorized logging with Asia/Jakarta timezone
- Memory-efficient connection management

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
git clone https://github.com/StephanieAgatha/grasschann

# Change directory
cd grasschann

# Install dependencies
go mod tidy

# Run the application
go run main.go
```

## Configuration

### Files Setup
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

### Connection Parameters

```go
MaxConnsPerHost:      1000
ReadTimeout:          30s
WriteTimeout:         30s
MaxIdleConnDuration:  5m
MaxConnDuration:      10m
MaxConnWaitTimeout:   30s
PingInterval:         26s
ReadDeadline:        50s
WriteDeadline:       20s
```

### Retry Mechanism
```go
MaxRetries:          10
BaseRetryDelay:      5s
MaxRetryDelay:       30s
RetryInterval:       20s
```

### Cache Settings
```go
CacheDuration:       5m
CacheCleanup:        10m
```

## Key Features

### 1. Connection Pool Management
- FastHTTP client pooling
- Memory-efficient resource utilization
- Connection reuse optimization
- Automatic cleanup of idle connections

### 2. Proxy Handling
- Support for both SOCKS5 and HTTP proxies
- Automatic proxy URL normalization
- Proxy health monitoring
- Even distribution among users

### 3. Error Handling
- Exponential backoff retry mechanism
- Automatic reconnection on failure
- Panic recovery in goroutines
- Comprehensive error logging

### 4. Performance Optimization
- FastHTTP for reduced memory allocation
- Connection pooling and reuse
- Write compression enabled
- Optimized buffer sizes
- Efficient proxy distribution

### 5. Monitoring
- Active connection tracking
- Real-time connection status
- Detailed error logging
- Response time monitoring
- Proxy health statistics
## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

1. Fork the project
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Author

- Stephanie Agatha
- GitHub: [@StephanieAgatha](https://github.com/StephanieAgatha)

## Disclaimer

This tool is for educational purposes only. Make sure to comply with the terms of service of any services you interact with.