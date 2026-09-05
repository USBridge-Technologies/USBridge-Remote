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
