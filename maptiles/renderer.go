package maptiles

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

type TileCoord struct {
	X, Y, Zoom uint64
	Tms        bool
	Layer      string
	Scale      string
	Url        string
}

type TileFetchResult struct {
	Coord   TileCoord
	BlobPNG []byte
}

type TileFetchRequest struct {
	Coord   TileCoord
	OutChan chan<- TileFetchResult
}

func (c *TileCoord) setTMS(tms bool) {
	if c.Tms != tms {
		c.Y = (1 << c.Zoom) - c.Y - 1
		c.Tms = tms
	}
}

func NewTileRendererChan(stylesheet string) chan<- TileFetchRequest {
	c := make(chan TileFetchRequest)

	go func(requestChan <-chan TileFetchRequest) {
		var err error
		t := NewTileRenderer(stylesheet)
		for request := range requestChan {
			go func(request TileFetchRequest, t *TileRenderer) {
				result := TileFetchResult{request.Coord, nil}
				result.BlobPNG, err = t.RenderTile(request.Coord)
				if err != nil {
					log.Println("Error while rendering", request.Coord, ":", err.Error())
					result.BlobPNG = nil
				}
				request.OutChan <- result
			}(request, t)
		}
	}(c)

	return c
}

// Renders images as Web Mercator tiles
type TileRenderer struct {
	m string
}

func NewTileRenderer(stylesheet string) *TileRenderer {
	t := new(TileRenderer)
	var err error
	if err != nil {
		log.Fatal(err)
	}

	return t
}

func (t *TileRenderer) RenderTile(c TileCoord) ([]byte, error) {
	c.setTMS(false)
	return t.RenderTileZXY(c.Zoom, c.X, c.Y, c.Scale, c.Layer, c.Url)
}

// Render a tile with coordinates in Google tile format.
// Most upper left tile is always 0,0. Method is not thread-safe,
// so wrap with a mutex when accessing the same renderer by multiple
// threads or setup multiple goroutinesand communicate with channels,
// see NewTileRendererChan.
func (t *TileRenderer) RenderTileZXY(zoom, x, y uint64, scale, layer, url string) ([]byte, error) {
	tile_url := strings.Replace(url, "{z}", fmt.Sprintf("%d", zoom), 1)
	tile_url = strings.Replace(tile_url, "{x}", fmt.Sprintf("%d", x), 1)
	tile_url = strings.Replace(tile_url, "{y}", fmt.Sprintf("%d%s", y, scale), 1)
	tile_url = strings.Replace(tile_url, "{layer}", layer, 1)
	// tile_url := fmt.Sprintf("http://thor.tux:13013/style/%d/%d/%d%s.png?id=tmstyle:///app/kelantan-tm2/%s.tm2", zoom, x, y, scale, layer)
	// fmt.Printf("%s\n", url)
	// TODO: handle 404/500 error from mapbox-studio
	var resp, err = http.Get(tile_url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
