package flagrouter

import (
	"context"
	"testing"
	"time"
)

func TestHandle(t *testing.T) {
	r := New("handle", "")
	var run bool
	r.Handle(func() { run = true })
	_, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
	if !run {
		t.Fatal("handle: not run")
	}
}

func TestHandleGroup(t *testing.T) {
	r := New("group", "")
	var run bool
	r.HandleGroup("test", "", func() { run = true })
	_, err := r.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
	if !run {
		t.Fatal("handle: not run")
	}
}

func TestUse(t *testing.T) {
	r := New("use", "")
	var run [3]bool
	r.Use(func() { run[0] = true })
	r.Use(func() { run[1] = true })
	r.Handle(func() { run[2] = true })
	_, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("use run: %v", err)
	}
	for i, r := range run {
		if !r {
			t.Fatalf("use: %vth not run: %v", i+1, run)
		}
	}
}

func TestUseNext(t *testing.T) {
	var id int
	incr := func() int {
		id++
		return id
	}

	r := New("next", "")
	var run [3]int
	r.Use(func(next func()) {
		run[0] = incr() // 1
		next()
	})
	r.Use(func(next func()) {
		next()
		run[1] = incr() // 3
	})
	r.Handle(func() {
		run[2] = incr() // 2
	})
	_, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("use run: %v", err)
	}

	if !(run[0] == 1 && run[1] == 3 && run[2] == 2) {
		t.Fatalf("next: run result: %v", run)
	}
}

var ctxKey = new(int)

func TestHandleContext(t *testing.T) {
	var bar any = 123

	r := New("handle_context", "")

	r.Handle(func(ctx context.Context) {
		if val := ctx.Value(ctxKey); val != bar {
			t.Fatalf("handle context: value: %v", val)
		}
	})

	_, err := r.Run(context.WithValue(context.Background(), ctxKey, bar))
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
}

type options struct {
	Int   int              `short:"i" long:"int" dft:"-111"`
	Uint  uint             `short:"u" long:"uint" dft:"999"`
	Float float64          `short:"f" long:"float" dft:"1.111"`
	Bool  bool             `short:"b" long:"bool" dft:"false"`
	Str   string           `short:"s" long:"str" dft:"abc"`
	Dur   time.Duration    `short:"d" long:"dur" dft:"1s"`
	Time  time.Time        `short:"t" long:"time" dft:"2024-01-02T15:04:05"`
	List  []int            `short:"l" long:"list" dft:"1,2,3"`
	Map   map[string]int   `short:"m" long:"map" dft:"a:1,b:2,c:3"`
	LM    []map[string]int `short:"x" long:"list-map" dft:"a:1,b:2,c:3;x:7,y:8,z:9"`
	ML    map[string][]int `short:"y" long:"map-list" dft:"a:1,a:2,a:3,b:4,b:5,b:6"`
}

func TestHandleOptions(t *testing.T) {
	r := New("handle_options", "")

	r.Handle(func(opt *options) {
		if opt.Int != 456 {
			t.Fatalf("handle options: option.Int: %v", opt.Int)
		}
	})

	_, err := r.Run(context.Background(), "-i", "456")
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
}

func TestHandleContextOptions(t *testing.T) {
	var bar any = 123

	r := New("handle_context", "")

	r.Handle(func(ctx context.Context, opt *options) {
		if val := ctx.Value(ctxKey); val != bar {
			t.Fatalf("handle context: value: %v", val)
		}
		if opt.Int != 456 {
			t.Fatalf("handle context: option.Int: %v", opt.Int)
		}
	})

	_, err := r.Run(context.WithValue(context.Background(), ctxKey, bar), "-i", "456")
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
}

func TestUseContext(t *testing.T) {
	var bar any = 123

	r := New("use_context", "")

	r.Use(func(ctx context.Context, next func(context.Context)) {
		next(context.WithValue(ctx, ctxKey, bar))
	})

	r.Handle(func(ctx context.Context) {
		if val := ctx.Value(ctxKey); val != bar {
			t.Fatalf("handle context: value: %v", val)
		}
	})

	_, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
}

func TestPosition(t *testing.T) {
	r := New("handle_position", "")

	r.Handle(func(opt *struct {
		Float float64 `dft:"1.111"`
		Int   int     `short:"i" long:"int" dft:"-111"`
		Bool  bool    `dft:"false"`
		Str   string  `dft:"abc"`
	}) {
		if opt.Float != 2.22 {
			t.Fatalf("handle options: option.Float: %v", opt.Float)
		}
		if opt.Int != 456 {
			t.Fatalf("handle options: option.Int: %v", opt.Int)
		}
		if opt.Bool != true {
			t.Fatalf("handle options: option.Bool: %v", opt.Bool)
		}
		if opt.Str != "def" {
			t.Fatalf("handle options: option.Str: %v", opt.Str)
		}
	})

	_, err := r.Run(context.Background(), "2.22", "-i", "456", "true", "def")
	if err != nil {
		t.Fatalf("handle run: %v", err)
	}
}
