# flagrouter

flagrouter是基于`github.com/eachain/flags`的参数解析框架。该库设计思路来源于[macaron](https://go-macaron.com)框架，旨在将参数解析框架化。



## 用法

flagrouter需要将参数列表定义为一个`struct`，格式示例如下：

```go
type arg struct {
	A int `short:"a" long:"all" dft:"123" desc:"what is a"`
}
```

支持的tag有：

- `short`：短参数，仅支持一个字符，取值范围为`[a-z,A-Z]`；
- `long`：长参数，一个字符串，不需要前缀`--`；
- `dft`：默认值，如果参数解析时不传该参数，则该字段被设定为默认值；
- `desc`：参数描述，描述该参数作用。

flagrouter支持中间件格式：

- `func()`
- `func(context.Context)`
- `func(arg)` or `func(*arg)`
- `func(handler func())`
- `func(context.Context, arg)` or `func(context.Context, *arg)`
- `func(arg, handler func())` or `func(*arg, handler func())`
- `func(context.Context, handler func())` or `func(context.Context, handler func(context.Context))`
- `func(context.Context, arg, handler func())` or `func(context.Context, *arg, handler func())`
- `func(context.Context, arg, handler func(context.Context))` or `func(context.Context, *arg, handler func(context.Context))`

flagrouter支持handler格式：

- `func()`
- `func(context.Context)`
- `func(arg)` or `func(*arg)`
- `func(context.Context, arg)` or `func(context.Context, *arg)`



## 示例

```go
// test.go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eachain/flagrouter"
)

type ConfigOptions struct {
	File string `short:"c" long:"config" dft:"app.cfg" desc:"config file"`
}

func main() {
	r := flagrouter.New(filepath.Base(os.Args[0]), "this is a test app desc")

	r.Stmt(func() {
		r.Use(func() {
			fmt.Println("stmt middleware: only valid for stmt cmd")
		})
		r.HandleGroup("stmt", "the stmt subcommand", func(opt *ConfigOptions) {
			fmt.Printf("stmt handler, config: %v\n", opt.File)
		})
	})

	r.Group("bar", "the first sub command", func() {
		r.Use(func(handler func()) {
			fmt.Printf("before bar\n")
			handler()
			fmt.Printf("bar quit\n")
		})
		r.Handle(func() { fmt.Println("bar") })
	})

	r.Use(func(opt *ConfigOptions) {
		fmt.Printf("config file: %v, used by all following cmds\n", opt.File)
	})

	r.Handle(func() {
		fmt.Println("main handler")
	})

	r.Group("foo", "the second sub command", func() {
		r.Use(func(ctx context.Context, handler func(context.Context)) {
			fmt.Printf("before foo\n")
			handler(context.WithValue(ctx, "foo", "1234567890"))
			fmt.Printf("foo quit\n")
		})
		r.Handle(func(ctx context.Context) {
			fmt.Printf("foo, context value: %v\n", ctx.Value("foo"))
		})
	})

	if usage, err := r.Run(context.Background(), os.Args[1:]...); err != nil {
		if errors.Is(err, flagrouter.ErrHelp) || errors.Is(err, flagrouter.ErrNoExecFunc) {
			fmt.Fprintln(os.Stderr, usage)
			return
		}

		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
```

执行`go run test.go -h`：

```bash
$ go run test.go -h
test - this is a test app desc

Usage:
  test [option|command]

Options:
  -c, --config string (default: "app.cfg")
    config file

Commands:
  stmt
    the stmt subcommand

  bar
    the first sub command

  foo
    the second sub command
```

执行`go run test.go`：

```bash
$ go run test.go   
config file: app.cfg, used by all following cmds
main handler
```

执行`go run test.go stmt -h`：

```bash
$ go run test.go stmt -h
test stmt - the stmt subcommand

Usage:
  test stmt [option]

Options:
  -c, --config string (default: "app.cfg")
    config file
```

执行`go run test.go stmt`：

```bash
$ go run test.go stmt   
stmt middleware: only valid for stmt cmd
stmt handler, config: app.cfg
```

执行`go run test.go foo`：

```bash
$ go run test.go foo   
config file: app.cfg, used by all following cmds
before foo
foo, context value: 1234567890
foo quit
```



### 参数不可重复注册

```go
// test.go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eachain/flagrouter"
)

type ConfigOptions struct {
	File string `short:"c" long:"config" dft:"app.cfg" desc:"config file"`
}

func main() {
	r := flagrouter.New(filepath.Base(os.Args[0]), "this is a test app desc")

	r.Use(func(opt *ConfigOptions) {
		fmt.Printf("config file: %v\n", opt.File)
	})

	r.Handle(func(opt *ConfigOptions) {
		fmt.Printf("main handler, config file: %v\n", opt.File)
	})

	if usage, err := r.Run(context.Background(), os.Args[1:]...); err != nil {
		if errors.Is(err, flagrouter.ErrHelp) || errors.Is(err, flagrouter.ErrNoExecFunc) {
			fmt.Fprintln(os.Stderr, usage)
			return
		}

		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
```

执行`go run test.go`：

```bash
$ go run test.go       
panic: flags: duplicated short option: -c

goroutine 1 [running]:
......
```

正确用法应该是，单独记一个变量存储需要重复用到的参数值：

```go
var configFile string
r.Use(func(opt *ConfigOptions) {
	configFile = opt.File
	fmt.Printf("config file: %v\n", opt.File)
})

r.Handle(func() {
	fmt.Printf("main handler, config file: %v\n", configFile)
})
```

实际上，应用程序应尽量避免这种情况。一个参数不应该由多个中间件或handler共同处理。