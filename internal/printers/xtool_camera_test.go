package printers

import (
	"bytes"
	"context"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/simcamera"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool/xtoolsim"
)

// blackFrame is what the F1 Ultra's camera sends after autofocus.
var blackFrame = simcamera.JPEG(image.NewGray(image.Rect(0, 0, 64, 48)))

func shortBlackWait(t *testing.T) {
	t.Helper()
	was := xtoolBlackWait
	xtoolBlackWait = time.Millisecond
	t.Cleanup(func() { xtoolBlackWait = was })
}

// TestAnXToolOnWSV2PhotographsItsBed: the F1 Ultra's camera, by name, the
// photo fetched over the file channel, after the fill light is turned up,
// past the black frames it sends after autofocus.
func TestAnXToolOnWSV2PhotographsItsBed(t *testing.T) {
	shortBlackWait(t)
	sim := xtoolSim(t, "40.52.016.05")
	photo := simcamera.Photo(simcamera.Workpiece)
	sim.Set(func(s *xtoolsim.V2) { s.Photos = [][]byte{blackFrame, blackFrame, photo} })
	if !xtoolModel(t, xtool.ModelF1Ultra).Camera {
		t.Fatal("the F1 Ultra has no camera")
	}
	m := xtoolMachineAt(t, xtool.ModelF1Ultra, sim.Addr(), `{"fill_light": 50}`)
	cam, ok := m.(Camera)
	if !ok {
		t.Fatal("the F1 Ultra's machine is not a Camera")
	}
	got, err := cam.Photo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, photo) {
		t.Errorf("got %d bytes, not the photo", len(got))
	}
	var fills []int
	sim.Set(func(s *xtoolsim.V2) { fills = slices.Clone(s.FillLights) })
	if !slices.Equal(fills, []int{128}) {
		t.Errorf("fill light set to %v, want [128]", fills)
	}
	// Nothing moved: no job, frame or action.
	if a := sim.Actions(); len(a) != 0 || sim.Framed() != 0 {
		t.Errorf("a photo moved the machine: %v", a)
	}

	// Only black frames, and it gives up, saying why.
	sim.Set(func(s *xtoolsim.V2) { s.Photos = [][]byte{blackFrame} })
	if _, err := cam.Photo(context.Background()); err == nil || !strings.Contains(err.Error(), "black frames") {
		t.Errorf("err = %v, want black frames", err)
	}
}

// TestAnXToolOnRESTV1PhotographsItsBed: stream 0 on the camera's port.
func TestAnXToolOnRESTV1PhotographsItsBed(t *testing.T) {
	sim := xtoolsim.NewREST(xtool.DialectProcessing, "40.52.016.04")
	defer sim.Close()
	photo := simcamera.Photo(simcamera.WorkpieceLarge)
	sim.Set(func(s *xtoolsim.REST) { s.Photos = [][]byte{photo} })
	port := sim.Addr()[strings.LastIndex(sim.Addr(), ":")+1:]
	m := xtoolMachineAt(t, xtool.ModelF1UltraV1, sim.Addr(), `{"camera_port": `+port+`}`)
	got, err := m.(Camera).Photo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, photo) {
		t.Errorf("got %d bytes, not the photo", len(got))
	}
	var fills []int
	sim.Set(func(s *xtoolsim.REST) { fills = slices.Clone(s.FillLights) })
	if !slices.Equal(fills, []int{255}) {
		t.Errorf("fill light set to %v, want the default, [255]", fills)
	}
}

// TestXToolModelsWithoutACameraSaySo: PROTOCOL.md gives no snapshot route
// for the F1 on WS-V2, or the M1.
func TestXToolModelsWithoutACameraSaySo(t *testing.T) {
	for _, id := range []string{xtool.ModelF1, xtool.ModelM1V1} {
		if xtoolModel(t, id).Camera {
			t.Errorf("the %s has a camera", id)
		}
	}
	sim := xtoolSim(t, "40.51.020.04")
	m := xtoolMachineAt(t, xtool.ModelF1, sim.Addr(), "")
	if _, err := m.(Camera).Photo(context.Background()); err == nil {
		t.Error("the F1 took a photo")
	}
	if err := Validate("xTool", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: xtool.ModelF1Ultra, Options: []byte(`{"fill_light": 101}`)}); err == nil {
		t.Error("a fill light of 101% is allowed")
	}
}
