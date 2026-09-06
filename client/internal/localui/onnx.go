package localui

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var initOnce sync.Once
var initErr error

// initRuntime points onnxruntime_go at a concrete libonnxruntime.so and
// initializes the ORT environment exactly once per process (the C API
// panics if InitializeEnvironment is called twice).
func initRuntime(sharedLibPath string) error {
	initOnce.Do(func() {
		if sharedLibPath != "" {
			ort.SetSharedLibraryPath(sharedLibPath)
		}
		initErr = ort.InitializeEnvironment()
	})
	return initErr
}

// session wraps one ONNX Runtime AdvancedSession for a single fixed-shape
// input/output pair -- all three models here (icon_detect, dbnet, svtr)
// take one float32 tensor in and produce one float32 tensor out.
type session struct {
	s          *ort.AdvancedSession
	inputT     *ort.Tensor[float32]
	outputT    *ort.Tensor[float32]
	inputShape ort.Shape
}

// newSession loads onnxPath and builds a session for a fixed input/output
// shape. useGPU requests the OpenVINO execution provider (Intel iGPU/CPU
// plugin selection is OpenVINO's own, driven by device); on any failure to
// initialize it (no compatible GPU, provider not present in this
// libonnxruntime.so, etc.) this transparently falls back to plain CPU --
// matching the device pattern of "degrade to a working default, don't hard
// fail the whole feature over one optional accelerator".
func newSession(onnxPath string, inputShape, outputShape []int64, useGPU bool) (*session, error) {
	inShape := ort.NewShape(inputShape...)
	outShape := ort.NewShape(outputShape...)

	inputT, err := ort.NewEmptyTensor[float32](inShape)
	if err != nil {
		return nil, fmt.Errorf("alloc input tensor: %w", err)
	}
	outputT, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		inputT.Destroy()
		return nil, fmt.Errorf("alloc output tensor: %w", err)
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		inputT.Destroy()
		outputT.Destroy()
		return nil, fmt.Errorf("session options: %w", err)
	}
	defer opts.Destroy()

	if useGPU {
		if err := opts.AppendExecutionProviderOpenVINO(map[string]string{
			"device_type": "GPU",
		}); err != nil {
			// Fall back to CPU silently -- see doc comment above.
			useGPU = false
		}
	}

	s, err := loadNamedSession(onnxPath, inputT, outputT, opts)
	if err != nil {
		inputT.Destroy()
		outputT.Destroy()
		return nil, err
	}

	return &session{s: s, inputT: inputT, outputT: outputT, inputShape: inShape}, nil
}

// loadNamedSession introspects onnxPath for its single input/output tensor
// names (all three models here have exactly one of each) and builds the
// AdvancedSession -- avoids hardcoding names like "x"/"images" that differ
// between the icon detector and the OCR models.
func loadNamedSession(onnxPath string, inputT, outputT *ort.Tensor[float32], opts *ort.SessionOptions) (*ort.AdvancedSession, error) {
	inputName, outputName, err := ort.GetInputOutputInfo(onnxPath)
	if err != nil {
		return nil, fmt.Errorf("inspect model %s: %w", onnxPath, err)
	}
	if len(inputName) != 1 || len(outputName) != 1 {
		return nil, fmt.Errorf("model %s: expected exactly 1 input and 1 output, got %d/%d", onnxPath, len(inputName), len(outputName))
	}
	return ort.NewAdvancedSession(onnxPath,
		[]string{inputName[0].Name}, []string{outputName[0].Name},
		[]ort.Value{inputT}, []ort.Value{outputT}, opts)
}

// run copies data into the input tensor, executes the session, and returns
// a copy of the output tensor's data.
func (s *session) run(data []float32) ([]float32, error) {
	copy(s.inputT.GetData(), data)
	if err := s.s.Run(); err != nil {
		return nil, err
	}
	out := s.outputT.GetData()
	return append([]float32(nil), out...), nil
}

func (s *session) Close() {
	if s.s != nil {
		s.s.Destroy()
	}
	if s.inputT != nil {
		s.inputT.Destroy()
	}
	if s.outputT != nil {
		s.outputT.Destroy()
	}
}

// batchedSession wraps a DynamicAdvancedSession -- unlike session (fixed
// shape, bound once at creation), this accepts a fresh input tensor with a
// different batch size on every call, output auto-allocated by ONNX
// Runtime. Built for SVTR: a real ui.parse call recognizes anywhere from a
// handful to a couple hundred text crops, and running them one at a time
// (a plain `session`, batch=1 always) spends most of its wall time on
// per-Run() overhead rather than actual compute -- benchmarked live
// (see internal/localui's package doc comment) at ~20ms/crop for 145
// crops serially (2.9s of a 4.1s total Parse call) vs. the same graph's
// isolated 7.2ms/crop at batch=1. Batching amortizes that overhead: at
// batch=16 on the OpenVINO GPU EP, throughput stabilizes at ~6.15ms/crop
// (vs. plain CPU actually getting WORSE per-crop above batch=8 for this
// model -- 14.6ms/crop at batch=16, 23.7ms/crop at batch=32, so GPU is
// used here even though single-crop SVTR was faster on CPU).
type batchedSession struct {
	s          *ort.DynamicAdvancedSession
	inputName  string
	outputName string
}

// svtrBatchSize is the batch size batchRecognizeSVTR groups crops into.
// Chosen from the benchmarked GPU numbers above: 16 is close to the
// per-crop-time knee (6.15ms/crop at 16 vs. 6.73ms/crop at 32, and higher
// batches mean more wasted compute padding out a call when the crop count
// doesn't divide evenly).
const svtrBatchSize = 16

func newBatchedSession(onnxPath string, useGPU bool) (*batchedSession, error) {
	inputName, outputName, err := ort.GetInputOutputInfo(onnxPath)
	if err != nil {
		return nil, fmt.Errorf("inspect model %s: %w", onnxPath, err)
	}
	if len(inputName) != 1 || len(outputName) != 1 {
		return nil, fmt.Errorf("model %s: expected exactly 1 input and 1 output, got %d/%d", onnxPath, len(inputName), len(outputName))
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("session options: %w", err)
	}
	defer opts.Destroy()
	if useGPU {
		if err := opts.AppendExecutionProviderOpenVINO(map[string]string{"device_type": "GPU"}); err != nil {
			// Fall back to CPU silently, same as newSession.
			useGPU = false
		}
	}

	s, err := ort.NewDynamicAdvancedSession(onnxPath,
		[]string{inputName[0].Name}, []string{outputName[0].Name}, opts)
	if err != nil {
		return nil, err
	}
	return &batchedSession{s: s, inputName: inputName[0].Name, outputName: outputName[0].Name}, nil
}

// run executes one batch: data must hold exactly batch*3*height*width
// float32 values (NCHW, batch outermost). Returns the flat output
// (batch*outPerItem floats) and lets the caller slice per-item.
func (b *batchedSession) run(data []float32, batch, channels, height, width int) ([]float32, error) {
	inShape := ort.NewShape(int64(batch), int64(channels), int64(height), int64(width))
	inputT, err := ort.NewTensor(inShape, data)
	if err != nil {
		return nil, fmt.Errorf("alloc batch input tensor: %w", err)
	}
	defer inputT.Destroy()

	outputs := []ort.Value{nil}
	if err := b.s.Run([]ort.Value{inputT}, outputs); err != nil {
		return nil, err
	}
	outT, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		outputs[0].Destroy()
		return nil, fmt.Errorf("unexpected output tensor type %T", outputs[0])
	}
	defer outT.Destroy()
	return append([]float32(nil), outT.GetData()...), nil
}

func (b *batchedSession) Close() {
	if b.s != nil {
		b.s.Destroy()
	}
}
