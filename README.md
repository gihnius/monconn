# monconn

A TCP connection monitoring tool written in Go.

## About

This is a tool(a library and a command line) for monitoring and debugging network services.

I built this for the purpose of facilitating debugging and monitoring the network connections of IoT devices. At this stage, it is just an experimental tool.

Note: For each tcp connection, an additional goroutine is used for monitoring, and uses a MonConn struct to store the monitored info, which takes up a bit more memory. For 5000 long connections, it takes up about 80~100MB of memory.

So it's not recommended to use it in high performance production environment.

**features**

- reject remote ip by managing an ip blacklist
- limit ip connections
- record upload/download traffics of all connections
- check idle tcp connection
- show connecting ip connections count
- show read and write bytes(hex array) on a connection
- gracefull stop a tcp listener

## Usage

### Install

library:

`go get -u -v github.com/gihnius/monconn`

command line:

`go get -u -v github.com/gihnius/monconn/cli/monconn`

then run: `monconn -h`

### Configuration

#### for library

``` go
import "github.com/gihnius/monconn"

// --------------- global setup
monconn.Debug = true // enable debug log
monconn.DebugFunc = func(format string, v ...interface{}) {
    // custom log function
}

// set net.Conn buffer size, default 4k
monconn.ReadBufSize = 4 << 10
monconn.WriteBufSize = 4 << 10

// set how many concurrency clients(ip) can connect to a service
// default 64
monconn.MaxIPLimit = 1000

// --------------- service setup
// set net.Conn read write timeout
service.ReadTimeout = 600
service.WriteTimeout = 600
// close wait timeout
service.WaitTimeout = 60
// max idle check in seconds
service.MaxIdle = 900
// connections count to a service, default 0, no limit
service.ConnLimit = 0 // ConnLimit changes need to restart service
// ips count to service, default 0, no limit
service.IPLimit = 0
// set tcp connection keepalive, default true
service.KeepAlive = true
// Print read write bytes in hex format, default false
service.PrintBytes = false

```

#### for command line

please checkout `monconn/cli/monconn/config.yaml`.

simple usage as a port forwarder:

``` shell
# use as ssh and web frontend to monitor traffics
$ monconn -p :22=127.0.0.1:2222 -p 0.0.0.0:80=127.0.0.1:8080
# here 2222 and 8080 are the real ssh and web services

```

more configurable options:

``` yaml
# monconn config.yaml
service_1: # service name
  # service listen on
  listen_ip: "127.0.0.1"
  # monconn listen on 80
  listen_port: 80
  # forward requests to this backend
  backend_ip: "127.0.0.1"
  # the real web listen on 8080
  backend_port: 8080
  # service config
  read_timeout: 600 # 10 minutes
  write_timeout: 600 # 10 minutes
  wait_timeout: 60 # 1 minute
  max_idle: 900 # 15 minutes
  conn_limit: 0 # zero means no limit
  ip_limit: 0 # zero means no limit
  keepalive: true
  print_bytes: true
  ip_blacklist:
    - 1.1.1.1
    - 2.2.2.2

```

### Example

``` go
// create service by given a sid: "127.0.0.1:1234"
// a service is binding to a tcp listener, so normally
// use a listen address as sid to identify a service
// you can also choose a uniqe string as sid.
service := monconn.NewService("127.0.0.1:1234")
// configure service like above
// service.ReadTimeout = ...
// listen a tcp port
// start the service monitor on ln
service.Listen("tcp", "127.0.0.1:1234")

// accept connection and monitor the connection
conn, err := service.AcquireConn()
if err != nil {
    // handle err
} else {
    go HandleConn(conn)
}

// when everything done, usually before program exit,
// call service.Stop() to stop the listener as well as service
service.Close()
// or call Shutdown() to Stop all services if there are multiple started.
monconn.Shutdown()

// or pls checkout the example code in examples/

// checkout the API to see how to grab the monitored infomation.

// ... that's all

```

## API

### monconn package api

- NewService(sid) to create a service
- GetService(sid) get service by sid
- DelService(sid) delete a service
- Shutdown() Stop all services

### Service instance method

- RejectIP(ip) add ip to blacklist
- ReleaseIP(ip) remove ip from blacklist
- Listen() start monitor the listener
- AcquireConn() return monitored new connection
- Close() stop service
- WrapMonConn()
- EliminateBytes(r, w) see godoc
- ReadWriteBytes() return how many bytes read or write in a service
- IPs() service's connecting ip
- Uptime() service's uptime, in seconds
- Sid() service's sid getter
- AccessAt() latest client connect time
- BootAt() service start from time
- ConnCount() how many realtime connections
- IPCount() realtime ips
- Stats() json format stats
- Log()

### MonConn instance method

- Idle() tell if client read idle
- Stats()
- Log()

see [godoc](https://godoc.org/github.com/gihnius/monconn)

## TODO

- a session wrapper
- improve logger
- Implement ReadFrom and WriteTo methods

## Status

In development (alpha).

## License
