package maptiles

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
)

type Generator struct {
	MapFile   string
	TileDir   string
	Threads   int
	LayerName string
	Format    string
	Url       string
}

type Coord struct {
	X, Y float64
}

func ensureDirExists(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.Mkdir(path, 0755)
	}
}

func (g *Generator) Run(lowLeft, upRight Coord, minZ, maxZ uint64, name string) {
	runtime.GOMAXPROCS(g.Threads)
	c := make(chan TileCoord)
	mc := make(chan int, g.Threads)

	log.Println("Starting job", name)

	ensureDirExists(g.TileDir)

	lmp := NewLayerMultiplex()
	layername := g.LayerName
	url := g.Url
	format := g.Format
	fn := fmt.Sprintf("%s/%s_%s.mbtiles", g.TileDir, layername, format)
	tdb := NewTileDb(fn)
	total := 0
	processed := 0
	lmp.AddRenderer(layername, url)
	for i := 0; i < g.Threads; i++ {
		go func(ctc <-chan TileCoord, mc chan int) {
			for tc := range ctc {
				ch := make(chan TileFetchResult)
				tr := TileFetchRequest{tc, ch}
				tdb.RequestQueue() <- tr
				result := <-ch
				processed = processed + 1
				if result.Blob == nil {
					// Tile was not provided by DB, so submit the tile request to the renderer
					lmp.SubmitRequest(tr)
					result := <-ch
					if result.Blob == nil {
						// The tile could not be rendered, now we need to bail out.
						fmt.Println("Not Found")
						return
					}
					// insert newly rendered tile into cache db
					tdb.InsertQueue() <- result
					percent := processed / total * 100
					fmt.Println("Insert", tc.Zoom, tc.X, tc.Y, "percent, processed/total: ", percent, processed, total)
				} else {
					fmt.Println("Cached", tc.Zoom, tc.X, tc.Y)
				}
			}
			mc <- 1
		}(c, mc)
	}

	ll0 := [2]float64{lowLeft.X, upRight.Y}
	ll1 := [2]float64{upRight.X, lowLeft.Y}

	for z := minZ; z <= maxZ; z++ {
		px0 := fromLLtoPixel(ll0, z)
		px1 := fromLLtoPixel(ll1, z)

		for x := uint64(math.Abs(px0[0]) / 256.0); x <= uint64(math.Abs(px1[0])/256.0); x++ {
			if x < 0 || x >= uint64(math.Pow(float64(2), float64(z))) {
				continue
			}
			for y := uint64(math.Abs(px0[1]) / 256.0); y <= uint64(math.Abs(px1[1])/256.0); y++ {
				if y < 0 || y >= uint64(math.Pow(float64(2), float64(z))) {
					continue
				}
				total = total + 1
			}
		}
	}
	for z := minZ; z <= maxZ; z++ {
		px0 := fromLLtoPixel(ll0, z)
		px1 := fromLLtoPixel(ll1, z)

		for x := uint64(math.Abs(px0[0]) / 256.0); x <= uint64(math.Abs(px1[0])/256.0); x++ {
			if x < 0 || x >= uint64(math.Pow(float64(2), float64(z))) {
				continue
			}
			for y := uint64(math.Abs(px0[1]) / 256.0); y <= uint64(math.Abs(px1[1])/256.0); y++ {
				if y < 0 || y >= uint64(math.Pow(float64(2), float64(z))) {
					continue
				}
				c <- TileCoord{x, y, z, false, layername, "", url, format}
			}
		}
	}
	close(c)
	for i := 0; i < g.Threads; i++ {
		<-mc
	}
}
