package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// Reproduces the 1.1 crash: the settings server changing a value while the UI thread reads
// widget positions. Before the fix this died with "concurrent map read and map write".
func TestSetFieldDoesNotTouchSharedMaps(t *testing.T) {
	cfg = defaultConfig()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the "UI thread": reads the map constantly, like every frame does
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = cfg.Widgets["vinyl"].X
			_ = cfg.Widgets["halo"].Scale
			for range cfg.Widgets {
			}
		}
	}()
	widgetsBefore := cfg.Widgets
	vinylBefore := cfg.Widgets["vinyl"]
	for i := 0; i < 2000; i++ {
		withCfg(func() {
			tmp := *cfg
			if err := tmp.setField("FPS", json.RawMessage("45")); err != nil {
				t.Error(err)
			}
		})
	}
	close(stop)
	wg.Wait()
	if len(cfg.Widgets) != len(widgetsBefore) || cfg.Widgets["vinyl"] != vinylBefore {
		t.Fatal("setField replaced or modified the shared widget map")
	}
	time.Sleep(10 * time.Millisecond)
}
