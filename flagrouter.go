package flagrouter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/eachain/flags"
)

type Router struct {
	fs *flags.FlagSet
}

func New(name, desc string) *Router {
	return &Router{
		fs: flags.New(name, desc),
	}
}

func Cmdline(desc string) *Router {
	return &Router{
		fs: flags.Cmdline(desc),
	}
}

// middleware must be one of following format:
//   - `func()`
//   - `func(context.Context)`
//   - `func(arg)` or `func(*arg)`
//   - `func(handler func())`
//   - `func(context.Context, arg)` or `func(context.Context, *arg)`
//   - `func(arg, handler func())` or `func(*arg, handler func())`
//   - `func(context.Context, handler func())` or `func(context.Context, handler func(context.Context))`
//   - `func(context.Context, arg, handler func())` or `func(context.Context, *arg, handler func())`
//   - `func(context.Context, arg, handler func(context.Context))` or `func(context.Context, *arg, handler func(context.Context))`
//
// and arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" dft:"123" desc:"what is a"`
//	}
func (r *Router) Use(middlewares ...any) {
	for _, mw := range middlewares {
		m, err := r.parseMiddleware(mw)
		if err != nil {
			panic(err)
		}
		r.fs.Use(m)
	}
}

// handler must be one of following format:
//   - `func()`
//   - `func(context.Context)`
//   - `func(arg)` or `func(*arg)`
//   - `func(context.Context, arg)` or `func(context.Context, *arg)`
//
// and arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" dft:"123" desc:"what is a"`
//	}
func (r *Router) Handle(handler any) {
	h, err := r.parseFunc(handler)
	if err != nil {
		panic(err)
	}
	r.fs.Handle(h)
}

// Group open a new cmd group, use closure to register subcommands.
func (r *Router) Group(name, desc string, closure func()) {
	fs := r.fs
	r.fs = fs.Cmd(name, desc)
	closure()
	r.fs = fs
}

// Stmt open a new empty statement, use closure to register subcommands.
// It is always used to register some middlewares those not influence other cmds.
func (r *Router) Stmt(closure func()) {
	fs := r.fs
	r.fs = fs.Stmt()
	closure()
	r.fs = fs
}

// handler must be one of following format:
//   - `func()`
//   - `func(context.Context)`
//   - `func(arg)` or `func(*arg)`
//   - `func(context.Context, arg)` or `func(context.Context, *arg)`
//
// and arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" dft:"123" desc:"what is a"`
//	}
func (r *Router) HandleGroup(name, desc string, handler any) {
	r.Group(name, desc, func() {
		r.Handle(handler)
	})
}

var (
	ErrNoExecFunc   = flags.ErrNoExecFunc
	ErrNoInputValue = flags.ErrNoInputValue
	ErrHelp         = flags.ErrHelp
)

var ctxRouterKey = new(int)

func getRouter(ctx context.Context) *Router {
	r, _ := ctx.Value(ctxRouterKey).(*Router)
	return r
}

func putRouter(ctx context.Context, r *Router) context.Context {
	return context.WithValue(ctx, ctxRouterKey, r)
}

// Run parse args and exec the subcommand.
func (r *Router) Run(ctx context.Context, args ...string) (string, error) {
	return r.fs.Run(putRouter(ctx, r), args...)
}

// RunCmdline parse os.args and exec the subcommand.
func (r *Router) RunCmdline(ctx context.Context) {
	r.fs.RunCmdline(putRouter(ctx, r))
}

// Parsed: return whether the var is parsed.
func (r *Router) Parsed(pointer any) bool {
	return r.fs.Parsed(pointer)
}

func Parsed(ctx context.Context, pointer any) bool {
	if r := getRouter(ctx); r != nil {
		return r.Parsed(pointer)
	}
	return false
}

var (
	typEmptyFunc      = reflect.TypeOf(func() {})
	typContext        = reflect.TypeOf(new(context.Context)).Elem()
	typHandler        = reflect.TypeOf(flags.Handler(func(ctx context.Context) {}))
	typHandlerFunc    = reflect.TypeOf(func(ctx context.Context) {})
	typMiddleware     = reflect.TypeOf(flags.Middleware(func(ctx context.Context, handler flags.Handler) {}))
	typMiddlewareFunc = reflect.TypeOf(func(ctx context.Context, handler flags.Handler) {})
)

// middleware must be one of following format:
//   - `func()`
//   - `func(context.Context)`
//   - `func(arg)` or `func(*arg)`
//   - `func(handler func())`
//   - `func(context.Context, arg)` or `func(context.Context, *arg)`
//   - `func(arg, handler func())` or `func(*arg, handler func())`
//   - `func(context.Context, handler func())` or `func(context.Context, handler func(context.Context))`
//   - `func(context.Context, arg, handler func())` or `func(context.Context, *arg, handler func())`
//   - `func(context.Context, arg, handler func(context.Context))` or `func(context.Context, *arg, handler func(context.Context))`
//
// and arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" dft:"123" desc:"what is a"`
//	}
func (r *Router) parseMiddleware(mw any) (flags.Middleware, error) {
	// fast path
	typ := reflect.TypeOf(mw)
	if m, err := r.parseMiddlewareFast(mw, typ); err != nil || m != nil {
		return m, err
	}

	// slow path
	if typ == nil || typ.Kind() != reflect.Func {
		return nil, errors.New("middleware must be a func")
	}

	if typ.NumOut() != 0 {
		return nil, errors.New("middleware func must return nothing")
	}

	if typ.NumIn() > 3 {
		return nil, errors.New("middleware func can only receive no more than 3 args in")
	}

	function := reflect.ValueOf(mw)
	if typ.NumIn() == 0 { // func()
		return func(ctx context.Context, handler flags.Handler) {
			function.Call(nil)
			handler(ctx)
		}, nil
	}

	arg0 := typ.In(0)
	if typ.NumIn() == 1 { // func(handler func()) or func(arg)
		if arg0 == typEmptyFunc || arg0.ConvertibleTo(typEmptyFunc) {
			return func(ctx context.Context, handler flags.Handler) {
				function.Call([]reflect.Value{
					reflect.ValueOf(func() { handler(ctx) }).Convert(arg0),
				})
			}, nil
		}
		// func(arg) or func(*arg)
		param, err := r.parseFuncArgs(arg0, "middleware")
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, handler flags.Handler) {
			function.Call([]reflect.Value{param})
			handler(ctx)
		}, nil
	}

	arg1 := typ.In(1)
	if typ.NumIn() == 2 {
		// func(context.Context, handler func())
		if arg0 == typContext {
			if arg1 == typEmptyFunc || arg1.ConvertibleTo(typEmptyFunc) { // func(context.Context, handler func())
				return func(ctx context.Context, handler flags.Handler) {
					function.Call([]reflect.Value{
						reflect.ValueOf(ctx),
						reflect.ValueOf(func() { handler(ctx) }).Convert(arg1),
					})
				}, nil
			}
			// func(context.Context, handler func(context.Context))
			if arg1 == typHandler || arg1.ConvertibleTo(typHandler) {
				return func(ctx context.Context, handler flags.Handler) {
					function.Call([]reflect.Value{
						reflect.ValueOf(ctx),
						reflect.ValueOf(handler).Convert(arg1),
					})
				}, nil
			}
			// func(context.Context, arg) or func(context.Context, *arg)
			param, err := r.parseFuncArgs(arg0, "middleware")
			if err != nil {
				return nil, err
			}
			return func(ctx context.Context, handler flags.Handler) {
				function.Call([]reflect.Value{
					reflect.ValueOf(ctx),
					param,
				})
				handler(ctx)
			}, nil
		}

		// func(arg, handler func()) or func(*arg, handler func())
		if !arg1.ConvertibleTo(typEmptyFunc) {
			return nil, errors.New("middleware func with option and handler, the handler must be a func with 0 args and 0 returns")
		}
		param, err := r.parseFuncArgs(arg0, "middleware")
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, handler flags.Handler) {
			function.Call([]reflect.Value{
				param,
				reflect.ValueOf(func() { handler(ctx) }).Convert(arg1),
			})
		}, nil
	}

	// func(context.Context, arg, handler func())` or `func(context.Context, arg, handler func(context.Context))
	arg2 := typ.In(2)
	if arg0 != typContext {
		return nil, errors.New("middleware with context and option and handler, the first arg must be a context")
	}
	if !(arg2.ConvertibleTo(typEmptyFunc) || arg2.ConvertibleTo(typHandler)) {
		return nil, errors.New("middleware with context and option and handler, the second arg must be a func() or func(context)")
	}
	param, err := r.parseFuncArgs(arg1, "middleware")
	if err != nil {
		return nil, err
	}
	if arg2.ConvertibleTo(typEmptyFunc) {
		return func(ctx context.Context, handler flags.Handler) {
			function.Call([]reflect.Value{
				reflect.ValueOf(ctx),
				param,
				reflect.ValueOf(func() { handler(ctx) }).Convert(arg2),
			})
		}, nil
	}
	return func(ctx context.Context, handler flags.Handler) {
		function.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			param,
			reflect.ValueOf(handler).Convert(arg2),
		})
	}, nil
}

func (r *Router) parseMiddlewareFast(mw any, typ reflect.Type) (flags.Middleware, error) {
	switch typ {
	case typEmptyFunc:
		return func(ctx context.Context, handler flags.Handler) {
			mw.(func())()
			handler(ctx)
		}, nil

	case typHandler:
		return func(ctx context.Context, handler flags.Handler) {
			mw.(flags.Handler)(ctx)
			handler(ctx)
		}, nil

	case typHandlerFunc:
		return func(ctx context.Context, handler flags.Handler) {
			mw.(func(context.Context))(ctx)
			handler(ctx)
		}, nil

	case typMiddleware:
		return func(ctx context.Context, handler flags.Handler) {
			mw.(flags.Middleware)(ctx, handler)
		}, nil

	case typMiddlewareFunc:
		return func(ctx context.Context, handler flags.Handler) {
			mw.(func(context.Context, flags.Handler))(ctx, handler)
		}, nil
	}

	switch {
	case typ.ConvertibleTo(typEmptyFunc):
		f := reflect.ValueOf(mw).Convert(typEmptyFunc).Interface().(func())
		return func(ctx context.Context, handler flags.Handler) {
			f()
			handler(ctx)
		}, nil

	case typ.ConvertibleTo(typHandler):
		h := reflect.ValueOf(mw).Convert(typHandler).Interface().(flags.Handler)
		return func(ctx context.Context, handler flags.Handler) {
			h(ctx)
			handler(ctx)
		}, nil

	case typ.ConvertibleTo(typMiddleware):
		m := reflect.ValueOf(mw).Convert(typMiddleware).Interface().(flags.Middleware)
		return m, nil
	}

	return nil, nil
}

// handler must be one of following format:
//   - `func()`
//   - `func(context.Context)`
//   - `func(arg)` or `func(*arg)`
//   - `func(context.Context, arg)` or `func(context.Context, *arg)`
//
// and arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" dft:"123" desc:"what is a"`
//	}
func (r *Router) parseFunc(fn any) (flags.Handler, error) {
	// fast path
	typ := reflect.TypeOf(fn)
	if h, err := r.parseFuncFast(fn, typ); err != nil || h != nil {
		return h, err
	}

	// slow path

	if typ == nil || typ.Kind() != reflect.Func {
		return nil, errors.New("handler must be a func")
	}
	if typ.NumOut() != 0 {
		return nil, errors.New("handler func must return nothing")
	}

	if typ.NumIn() > 2 {
		return nil, errors.New("handler func can only receive 0 or 1 or 2 arg in")
	}

	function := reflect.ValueOf(fn)
	if typ.NumIn() == 0 { // func()
		return func(context.Context) {
			function.Call(nil)
		}, nil
	}

	arg0 := typ.In(0)
	if typ.NumIn() == 1 {
		// func(arg) or func(*arg)
		param, err := r.parseFuncArgs(arg0, "handler")
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) {
			function.Call([]reflect.Value{param})
		}, nil
	}

	// func(context.Context, arg) or func(context.Context, *arg)`
	if arg0 != typContext {
		return nil, errors.New("handler func with 2 args in, the first arg must be a context.Context")
	}
	param, err := r.parseFuncArgs(typ.In(1), "handler")
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) {
		function.Call([]reflect.Value{reflect.ValueOf(ctx), param})
	}, nil
}

func (r *Router) parseFuncFast(fn any, typ reflect.Type) (flags.Handler, error) {
	switch typ {
	case typEmptyFunc:
		return func(ctx context.Context) { fn.(func())() }, nil

	case typHandler:
		return fn.(flags.Handler), nil

	case typHandlerFunc:
		return fn.(func(context.Context)), nil
	}

	switch {
	case typ.ConvertibleTo(typEmptyFunc):
		f := reflect.ValueOf(fn).Convert(typEmptyFunc).Interface().(func())
		return func(context.Context) { f() }, nil

	case typ.ConvertibleTo(typHandler):
		return reflect.ValueOf(fn).Convert(typEmptyFunc).Interface().(flags.Handler), nil
	}

	return nil, nil
}

func (r *Router) parseFuncArgs(arg reflect.Type, who string) (reflect.Value, error) {
	isPtr := false
	if arg.Kind() == reflect.Pointer {
		isPtr = true
		arg = arg.Elem()
	}
	if arg.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%v func arg must be a struct", who)
	}
	return r.parseOptions(arg, isPtr)
}

// arg must be like:
//
//	struct {
//		A int `short:"a" long:"all" desc:"what is a" dft:"123"`
//	}
func (r *Router) parseOptions(arg reflect.Type, isPtr bool) (reflect.Value, error) {
	val := reflect.New(arg)
	ret := val
	val = val.Elem()
	if !isPtr {
		ret = val
	}

	for i := 0; i < val.NumField(); i++ {
		err := r.parseField(arg.Field(i), val.Field(i))
		if err != nil {
			return ret, err
		}
	}

	return ret, nil
}

func (r *Router) parseField(field reflect.StructField, val reflect.Value) error {
	if !field.IsExported() {
		return nil
	}

	short, long, dft, zeroDft, desc, sep, err := parseTag(field)
	if err != nil {
		return err
	}
	if short == 0 && long == "" {
		return nil
	}
	if dft != nil {
		dft = reflect.ValueOf(dft).Convert(field.Type).Interface()
	}

	opts := make([]flags.Options, 0, len(sep)+1)
	if len(sep) > 0 {
		opts = append(opts, flags.WithSliceSeperator(sep[0]))
	}
	if len(sep) > 1 {
		opts = append(opts, flags.WithKeyValueSeperator(sep[1]))
	}
	opts = append(opts, flags.WithZeroDefault(zeroDft))

	r.fs.AnyVar(val.Addr().Interface(), short, long, dft, desc, opts...)
	return nil
}

func parseTag(field reflect.StructField) (short byte, long string, dft any, zeroDft bool, desc string, sep []string, err error) {
	if tagShort := field.Tag.Get("short"); tagShort != "" {
		if len(tagShort) > 1 {
			err = fmt.Errorf("flagrouter: invalid short tag %q: length must be 1", tagShort)
			return
		}
		short = tagShort[0]
	}

	long = field.Tag.Get("long")

	if seperator := strings.TrimSpace(field.Tag.Get("sep")); seperator != "" {
		sep = make([]string, len(seperator))
		for i := 0; i < len(seperator); i++ {
			sep[i] = string(seperator[i])
		}
	}

	tagDft, zeroDft := field.Tag.Lookup("dft")
	if tagDft != "" {
		dft, err = parseDefault(field.Type, tagDft, sep...)
		if err != nil {
			return
		}
	}

	desc = field.Tag.Get("desc")

	return
}

var (
	typParser   = reflect.TypeOf(new(flags.Parser)).Elem()
	typDuration = reflect.TypeOf(time.Duration(0))
	typDateTime = reflect.TypeOf(time.Time{})
)

func parseDefault(typ reflect.Type, dft string, sep ...string) (any, error) {
	if reflect.PointerTo(typ).Implements(typParser) {
		val := reflect.New(typ)
		err := val.Interface().(flags.Parser).ParseFlag(dft)
		if err != nil {
			return nil, err
		}
		return val.Elem().Interface(), nil
	}

	switch typ {
	case typDuration:
		return time.ParseDuration(dft)
	case typDateTime:
		return time.ParseInLocation(flags.DateTime, dft, time.Local)
	}

	switch typ.Kind() {
	default:
		return nil, fmt.Errorf("flagrouter: unsupported type: %v", typ)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.ParseInt(dft, 10, 64)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.ParseUint(dft, 10, 64)

	case reflect.Float32, reflect.Float64:
		return strconv.ParseFloat(dft, 64)

	case reflect.Bool:
		return strconv.ParseBool(dft)

	case reflect.String:
		return dft, nil

	case reflect.Slice:
		elemTyp := typ.Elem()
		seperator := ","
		if len(sep) > 0 && sep[0] != "" {
			seperator = sep[0]
		}
		if elemTyp.Kind() == reflect.Map {
			seperator = ";"
			if len(sep) > 2 && sep[2] != "" {
				seperator = sep[2]
			}
		}
		elems := strings.Split(dft, seperator)
		ls := reflect.MakeSlice(typ, 0, len(elems))
		for _, elem := range elems {
			val, err := parseDefault(elemTyp, strings.TrimSpace(elem), sep...)
			if err != nil {
				return nil, err
			}
			ls = reflect.Append(ls, reflect.ValueOf(val).Convert(elemTyp))
		}
		return ls.Interface(), nil

	case reflect.Map:
		m := reflect.MakeMap(typ)
		sepElem := ","
		if len(sep) > 0 && sep[0] != "" {
			sepElem = sep[0]
		}
		sepKV := ":"
		if len(sep) > 1 && sep[1] != "" {
			sepKV = sep[1]
		}
		kt := typ.Key()
		vt := typ.Elem()
		for _, elem := range strings.Split(dft, sepElem) {
			kv := strings.Split(elem, sepKV)
			if len(kv) != 2 {
				return nil, fmt.Errorf("flagrouter: cannot convert %q to key value pair", elem)
			}
			key, err := parseDefault(kt, strings.TrimSpace(kv[0]), sep...)
			if err != nil {
				return nil, err
			}
			val, err := parseDefault(vt, strings.TrimSpace(kv[1]), sep...)
			if err != nil {
				return nil, err
			}
			if vt.Kind() == reflect.Slice {
				if ori := m.MapIndex(reflect.ValueOf(key).Convert(kt)); ori.IsValid() {
					m.SetMapIndex(reflect.ValueOf(key).Convert(kt), reflect.AppendSlice(ori, reflect.ValueOf(val).Convert(vt)))
				} else {
					m.SetMapIndex(reflect.ValueOf(key).Convert(kt), reflect.ValueOf(val).Convert(vt))
				}
			} else {
				m.SetMapIndex(reflect.ValueOf(key).Convert(kt), reflect.ValueOf(val).Convert(vt))
			}
		}
		return m.Interface(), nil
	}
}
