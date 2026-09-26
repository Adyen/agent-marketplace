package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

const (
	DefaultDeadline      = 30 * time.Second
	DefaultOutputCap     = 1 << 20
	DefaultHostCallLimit = 100
)

type Limits struct {
	Deadline      time.Duration
	OutputCap     int
	HostCallLimit int
}

func (l Limits) withDefaults() Limits {
	if l.Deadline <= 0 {
		l.Deadline = DefaultDeadline
	}
	if l.OutputCap <= 0 {
		l.OutputCap = DefaultOutputCap
	}
	if l.HostCallLimit <= 0 {
		l.HostCallLimit = DefaultHostCallLimit
	}
	return l
}

type Result struct {
	Value     json.RawMessage `json:"value"`
	Truncated bool            `json:"truncated,omitempty"`
}

type Bindings map[string]any

type interruptSentinel struct{}

type thrownValue struct{ v goja.Value }

func (t thrownValue) Error() string {
	if t.v == nil {
		return "promise rejected"
	}
	return t.v.String()
}

func Run(ctx context.Context, code string, binds Bindings, lim Limits) (Result, error) {
	return RunWithBinder(ctx, code, func(context.Context, *goja.Runtime, *int64, int) (Bindings, error) { return binds, nil }, lim)
}

func RunWithBinder(ctx context.Context, code string, binder func(context.Context, *goja.Runtime, *int64, int) (Bindings, error), lim Limits) (Result, error) {
	lim = lim.withDefaults()
	vm := goja.New()
	runCtx, cancel := context.WithTimeout(ctx, lim.Deadline)
	defer cancel()

	var calls int64
	binds, err := binder(runCtx, vm, &calls, lim.HostCallLimit)
	if err != nil {
		return Result{}, err
	}

	for name, value := range binds {
		if err := vm.Set(name, wrapHostCalls(vm, value, &calls, lim.HostCallLimit)); err != nil {
			return Result{}, CodeError(ErrCodeScriptError, "install binding %s: %v", name, err)
		}
	}

	done := make(chan struct{})
	sentinel := &interruptSentinel{}
	interrupt := func() {
		select {
		case <-done:
		default:
			vm.Interrupt(sentinel)
		}
	}
	go func() {
		select {
		case <-runCtx.Done():
			interrupt()
		case <-done:
		}
	}()
	defer close(done)

	v, err := vm.RunString("(" + code + ")")
	if err != nil {
		return Result{}, mapRunError(err, sentinel)
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return Result{}, CodeError(ErrCodeScriptError, "code must evaluate to a function")
	}
	v, err = fn(goja.Undefined())
	if err != nil {
		return Result{}, mapRunError(err, sentinel)
	}

	if p, isPromise := v.Export().(*goja.Promise); isPromise {
		if p.State() == goja.PromiseStateRejected {
			return Result{}, mapRunError(thrownValue{v: p.Result()}, sentinel)
		}
		v = p.Result()
	}

	stringify, ok := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("stringify"))
	if !ok {
		return Result{}, CodeError(ErrCodeScriptError, "JSON.stringify unavailable")
	}
	out, err := stringify(goja.Undefined(), v)
	if err != nil {
		return Result{}, mapRunError(err, sentinel)
	}
	raw := out.String()
	if len(raw) > lim.OutputCap {
		marker, err := json.Marshal(map[string]any{
			"truncated": true,
			"prefix":    raw[:lim.OutputCap],
		})
		if err != nil {
			return Result{}, CodeError(ErrCodeScriptError, "marshal truncation marker: %v", err)
		}
		return Result{Value: marker, Truncated: true}, nil
	}
	return Result{Value: json.RawMessage(raw)}, nil
}

func mapRunError(err error, sentinel *interruptSentinel) error {
	var ce *Error
	if errors.As(err, &ce) {
		return ce
	}
	var tv thrownValue
	if errors.As(err, &tv) {
		if thrown, ok := tv.v.Export().(*Error); ok {
			return thrown
		}
	}
	var exc *goja.Exception
	if errors.As(err, &exc) {
		if thrown, ok := exc.Value().Export().(*Error); ok {
			return thrown
		}
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if _, ok := ie.Value().(*interruptSentinel); ok {
			return CodeError(ErrCodeTimeout, "execution deadline reached")
		}
		return CodeError(ErrCodeTimeout, "execution interrupted: %v", ie.Value())
	}
	return CodeError(ErrCodeScriptError, "%s", sanitizeHostPaths(err.Error()))
}

func sanitizeHostPaths(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		for _, marker := range []string{"/internal/codemode/", "/pkg/mod/", "/internal/mcpclient/"} {
			if strings.Contains(f, marker) {
				fields[i] = "<host>"
				break
			}
		}
	}
	return strings.Join(fields, " ")
}

func throwCoded(vm *goja.Runtime, err *Error) {
	panic(vm.ToValue(err))
}

func wrapHostCalls(vm *goja.Runtime, value any, counter *int64, limit int) any {
	switch t := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[k] = wrapHostCalls(vm, v, counter, limit)
		}
		return out
	case func(goja.FunctionCall) goja.Value:
		fn := t
		return func(call goja.FunctionCall) goja.Value {
			countHostCall(vm, counter, limit)
			return fn(call)
		}
	default:
		rv := reflect.ValueOf(value)
		if rv.IsValid() && rv.Kind() == reflect.Func {
			return countedNativeFunc(vm, rv, counter, limit)
		}
		return value
	}
}

func countHostCall(vm *goja.Runtime, counter *int64, limit int) {
	if atomic.AddInt64(counter, 1) > int64(limit) {
		panic(vm.ToValue(CodeError(ErrCodeHostCallLimit, "host-call cap of %d reached", limit)))
	}
}

func countedNativeFunc(vm *goja.Runtime, fn reflect.Value, counter *int64, limit int) func(goja.FunctionCall) goja.Value {
	t := fn.Type()
	return func(call goja.FunctionCall) goja.Value {
		countHostCall(vm, counter, limit)
		in := make([]reflect.Value, 0, t.NumIn())
		for i := 0; i < t.NumIn(); i++ {
			arg := call.Argument(i)
			var dst any
			if err := vm.ExportTo(arg, &dst); err != nil {
				panic(vm.ToValue(fmt.Sprintf("argument %d not exportable", i)))
			}
			iv := reflect.New(t.In(i))
			raw, _ := json.Marshal(dst)
			if err := json.Unmarshal(raw, iv.Interface()); err != nil {
				panic(vm.ToValue(fmt.Sprintf("argument %d type mismatch: %v", i, err)))
			}
			in = append(in, iv.Elem())
		}
		outs := fn.Call(in)
		if len(outs) == 0 {
			return goja.Undefined()
		}
		if len(outs) == 2 {
			if e, ok := outs[1].Interface().(error); ok && e != nil {
				panic(vm.ToValue(e.Error()))
			}
			return vm.ToValue(outs[0].Interface())
		}
		return vm.ToValue(outs[0].Interface())
	}
}
