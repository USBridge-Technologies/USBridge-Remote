package benchrender

import "testing"

// What benchvideo builds is what the helper parses.
func TestArgsRoundTrip(t *testing.T) {
	args := Args(144, &Rect{X: -1920, Y: 0, W: 1920, H: 1080})
	if args[0] != HelperArg {
		t.Fatalf("args %v don't start with %s", args, HelperArg)
	}
	o, err := parse(args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if o.fps != 144 || o.rect == nil || *o.rect != (Rect{-1920, 0, 1920, 1080}) {
		t.Fatalf("parsed %+v (rect %+v)", o, o.rect)
	}
	if o, err := parse(Args(DefaultFPS, nil)[1:]); err != nil || o.rect != nil {
		t.Fatalf("no rect: %+v, %v", o, err)
	}
	for _, bad := range [][]string{{"-fps", "0"}, {"-rect", "1,2,3"}, {"-rect", "0,0,0,10"}} {
		if _, err := parse(bad); err == nil {
			t.Errorf("parse(%v) accepted", bad)
		}
	}
}

// Both boxes move every frame at 240fps, stay on screen and never overlap.
func TestBoxesMoveEveryFrame(t *testing.T) {
	const w, h, fps = 3840, 2160, 240
	prev := boxes(0, w, h)
	for i := 1; i < 5*fps; i++ {
		cur := boxes(float64(i)/fps, w, h)
		for j, b := range cur {
			if b.x < 0 || b.y < 0 || b.x+b.w > w || b.y+b.h > h {
				t.Fatalf("frame %d box %d off screen: %+v", i, j, b)
			}
			if b == prev[j] {
				t.Fatalf("frame %d box %d didn't move", i, j)
			}
		}
		if a, b := cur[0], cur[1]; a.y+a.h > b.y {
			t.Fatalf("frame %d: boxes overlap", i)
		}
		prev = cur
	}
}
