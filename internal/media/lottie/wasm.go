package lottie

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed thorvg.wasm
var thorvgWASM []byte

// memoryLimitPages caps one render's linear memory at 64 MiB.
//
// The module instantiates with 268 pages (16.8 MiB) and, measured against both
// an animated sticker and a 200-layer document, never grows past that — a
// normal render does not reach the heap-growth hook at all. The cap is
// headroom for pathological input, not a working limit, and it cannot go below
// the module's own initial memory. ThorVG handles a refused allocation by
// failing the render, so hitting it surfaces as an error rather than host
// memory pressure.
const memoryLimitPages = 1024

// engine owns the compiled module. Compiling costs ~260ms and instantiating
// costs ~1ms, so the compile happens once per process and every render gets a
// fresh instance: a sticker cannot observe or corrupt another sticker's linear
// memory, and the memory is reclaimed when the instance closes.
type engine struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
}

var loadEngine = sync.OnceValues(func() (*engine, error) {
	// A background context outlives individual renders; per-call deadlines are
	// carried by the context passed to each exported function.
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		// Lets a cancelled or expired context abort a render mid-flight
		// instead of blocking the goroutine until ThorVG finishes on its own.
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(memoryLimitPages))
	compiled, err := runtime.CompileModule(ctx, thorvgWASM)
	if err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("compile thorvg module: %w", err)
	}
	if err := registerHostImports(ctx, runtime, compiled); err != nil {
		_ = runtime.Close(ctx)
		return nil, err
	}
	return &engine{runtime: runtime, compiled: compiled}, nil
})

// registerHostImports answers the module's imports.
//
// ThorVG's software render path needs exactly one of them to do real work: the
// heap-growth hook. The rest are the Emscripten C runtime, the WebGL and
// WebGPU bindings and embind's registration hooks, and they are answered with
// zeros.
//
// Zeros rather than a trap, because the module does reach a few of them on
// real content — hand-written fixtures never do, and a corpus of 119 published
// Noto animated emoji reached two. Rendering stays correct when they are
// answered this way; trapping instead turned those documents into failed
// renders. Every reached import is counted so that a ThorVG upgrade which
// starts depending on one is visible rather than silent; TestRealWorldLottie
// asserts the set stays the size it is.
func registerHostImports(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule) error {
	builder := runtime.NewHostModuleBuilder("a")
	seenResizeHeap := false
	for _, imported := range compiled.ImportedFunctions() {
		module, name, _ := imported.Import()
		if module != "a" {
			return fmt.Errorf("thorvg module imports from unexpected namespace %q", module)
		}
		fn := stubImport(name, len(imported.ResultTypes()))
		if name == resizeHeapImport {
			seenResizeHeap = true
			fn = resizeHeap
		}
		builder = builder.NewFunctionBuilder().
			WithGoModuleFunction(fn, imported.ParamTypes(), imported.ResultTypes()).
			Export(name)
	}
	if !seenResizeHeap {
		return fmt.Errorf("thorvg module does not import %q; the vendored build changed", resizeHeapImport)
	}
	_, err := builder.Instantiate(ctx)
	return err
}

// reachedImports records which stubbed imports the module has actually called.
var reachedImports sync.Map

// ReachedHostImports reports the stubbed imports reached so far, by name and
// call count. It is diagnostic: the set is expected to stay small and stable,
// and growing means a ThorVG build has started relying on host behaviour this
// package does not provide.
func ReachedHostImports() map[string]int64 {
	out := map[string]int64{}
	reachedImports.Range(func(key, value any) bool {
		out[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})
	return out
}

func stubImport(name string, results int) api.GoModuleFunc {
	counter := &atomic.Int64{}
	return func(_ context.Context, _ api.Module, stack []uint64) {
		if counter.Add(1) == 1 {
			reachedImports.Store(name, counter)
		}
		for i := range results {
			stack[i] = 0
		}
	}
}

// resizeHeap implements emscripten_resize_heap: grow linear memory to at least
// the requested size, reporting whether the request was met.
var resizeHeap api.GoModuleFunc = func(_ context.Context, module api.Module, stack []uint64) {
	requested := uint32(stack[0])
	current := module.Memory().Size()
	if requested <= current {
		stack[0] = 1
		return
	}
	pages := (requested-current+pageSize-1)/pageSize + 1
	if _, ok := module.Memory().Grow(pages); !ok {
		// Over the configured limit. Reporting failure lets ThorVG's allocator
		// fail the render cleanly.
		stack[0] = 0
		return
	}
	stack[0] = 1
}

const pageSize = 65536

// instance is one module instantiation plus the exports this package drives.
type instance struct {
	module api.Module
	memory api.Memory
	fns    map[string]api.Function
}

func (e *engine) instantiate(ctx context.Context) (*instance, error) {
	module, err := e.runtime.InstantiateModule(ctx, e.compiled,
		wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize"))
	if err != nil {
		return nil, fmt.Errorf("instantiate thorvg module: %w", err)
	}
	in := &instance{module: module, memory: module.Memory(), fns: make(map[string]api.Function, len(exportNames))}
	for name, mangled := range exportNames {
		fn := module.ExportedFunction(mangled)
		if fn == nil {
			_ = module.Close(ctx)
			return nil, fmt.Errorf("thorvg export %s (%s) is missing; the vendored build changed", name, mangled)
		}
		in.fns[name] = fn
	}
	return in, nil
}

func (in *instance) close(ctx context.Context) { _ = in.module.Close(ctx) }

// call invokes a ThorVG entry point. A trapped import, an expired context or a
// guest fault all surface here as an error.
func (in *instance) call(ctx context.Context, name string, args ...uint64) (uint64, error) {
	fn, ok := in.fns[name]
	if !ok {
		return 0, fmt.Errorf("thorvg entry point %s is not in the export table", name)
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0], nil
}

// check invokes an entry point that returns Tvg_Result and rejects failures.
// tolerate lists result codes the caller treats as success.
func (in *instance) check(ctx context.Context, name string, tolerate []uint64, args ...uint64) error {
	result, err := in.call(ctx, name, args...)
	if err != nil {
		return err
	}
	if result == resultSuccess {
		return nil
	}
	for _, ok := range tolerate {
		if result == ok {
			return nil
		}
	}
	return fmt.Errorf("%s returned Tvg_Result %d", name, result)
}

// alloc copies bytes into guest memory and returns the pointer. The allocation
// lives as long as the instance, which is freed wholesale on close.
func (in *instance) alloc(ctx context.Context, data []byte) (uint32, error) {
	if len(data) >= math.MaxUint32 {
		return 0, errors.New("thorvg allocation exceeds the guest address space")
	}
	length := uint32(len(data)) //nolint:gosec // G115: bounded immediately above.
	// The trailing NUL lets the same allocation serve ThorVG's C-string
	// parameters without a second copy.
	pointer, err := in.mallocGuest(ctx, uint64(length)+1)
	if err != nil {
		return 0, err
	}
	if !in.memory.Write(pointer, data) || !in.memory.WriteByte(pointer+length, 0) {
		return 0, errors.New("thorvg memory write out of range")
	}
	return pointer, nil
}

// mallocGuest allocates inside the module and returns a validated pointer.
// Addresses in a wasm32 module are 32-bit; anything wider means the module is
// not the one this package was written against.
func (in *instance) mallocGuest(ctx context.Context, size uint64) (uint32, error) {
	raw, err := in.call(ctx, "malloc", size)
	if err != nil {
		return 0, err
	}
	if raw == 0 {
		return 0, errors.New("thorvg ran out of memory")
	}
	if raw > math.MaxUint32 {
		return 0, errors.New("thorvg returned an out-of-range pointer")
	}
	return uint32(raw), nil
}
