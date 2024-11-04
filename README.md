# Grass Bot - WebSocket Proxy Manager

A high-performance WebSocket proxy manager written in Go, designed for handling multiple proxy connections with efficient resource management.

## Features

- Multiple proxy support per user ID
- Automated proxy rotation and distribution
- Real-time WebSocket communication
- IP geolocation checking
- Efficient connection pooling
- Colorized logging with timezone support

## Core Components

1. **Connection Pool Management**
   ```go
   - FastHTTPClientPool: Manages HTTP client connections
   - Connection metrics tracking
   - Configurable timeouts and retry attempts
   - Memory-efficient pooling implementation
   ```

2. **Proxy Handling**
   ```go
   - Proxy URL normalization
   - Supports SOCKS5 and HTTP proxies
   - Credential handling
   - Distribution system for multiple users
   ```

3. **Caching System**
   ```go
   - TTL-based caching for IP info (5m duration)
   - Automatic cleanup mechanism (10m interval)
   - Thread-safe operations
   - Memory-efficient storage
   ```

4. **WebSocket Management**
   ```go
   - Ping/Pong mechanism (26s interval)
   - Write compression enabled
   - Optimized buffer sizes (4096 bytes)
   - Maximum message size (32MB)
   ```

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

## Performance Optimizations

1. **Memory Usage**
   ```go
   - Connection pooling with sync.Pool
   - Request/Response pooling
   - Pre-allocated response time slices
   - Disabled default headers
   ```

2. **Network Efficiency**
   ```go
   - FastHTTP implementation
   - Connection reuse
   - Compression enabled
   - Buffer optimization
   ```

3. **Error Resilience**
   ```go
   - Exponential backoff retry
   - Panic recovery
   - Graceful shutdowns
   - Context cancellation support
   ```

## Technical Configuration

```go
MaxConnsPerHost:      1000
ReadTimeout:          30s
WriteTimeout:         30s
MaxIdleConnDuration:  5m
MaxConnDuration:      10m
MaxConnWaitTimeout:   30s
RetryInterval:        20s
CacheDuration:        5m
CacheCleanup:         10m
RetryAttempts:        3
BufferSize:          4096
MaxMessageSize:       32MB
```

## Project Structure

```
├── main.go                # Main application entry
├── proxy.txt              # Proxy list file
├── uid.txt                # User ID list file
└── README.md              # Documentation
```

## Usage

```bash
# Run the application
go run main.go
```

## Logging

The application uses Zap logger with:
- Colorized output
- Asia/Jakarta timezone support
- Structured logging format
- Error tracking with stack traces
- Connection status monitoring
- Request/Response metrics

## Error Handling

The application implements comprehensive error handling:
- Automatic reconnection on failure
- Configurable retry intervals with exponential backoff
- Detailed error logging with stack traces
- Connection timeout management
- Panic recovery in goroutines
- Context cancellation handling

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