package mock

import (
	"log"
	"testing"
)

func TestStartStop(t *testing.T) {
	cases := map[string]struct {
		count         int
		shutdownAgain bool
		expectErr     bool
	}{
		"TargetStartStop": {
			count:     3,
			expectErr: false,
		},
		"TargetStop": {
			count:         1,
			expectErr:     true,
			shutdownAgain: true,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < tt.count; i++ {
				bs := &remoteBs{}
				err := bs.Startup("store1", "", "127.0.0.1", 2147483648, 512)
				if err != nil {
					log.Fatal("Failed to initialize tgt, err: ", err)
				}

				expectErr := false
				err = bs.Shutdown()
				if err != nil {
					expectErr = true
				}

				if tt.shutdownAgain {
					err = bs.Shutdown()
					if err != nil {
						expectErr = true
					}
				}

				if tt.expectErr != expectErr {
					t.Fatalf("Startup test failed, err: %v", err)
				}
			}
		})
	}
}
