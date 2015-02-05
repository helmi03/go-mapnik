package maptiles

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// TODO serve list of registered layers per HTTP (preferably leafletjs-compatible js-array)

// Handles HTTP requests for map tiles, caching any produced tiles
// in an MBtiles 1.2 compatible sqlite db.
type TileServer struct {
	m         map[string]*TileDb
	lmp       *LayerMultiplex
	TmsSchema bool
	// cacheFile string
	url     string
	basedir string
}

func NewTileServer(url, basedir string) *TileServer {
	t := TileServer{}
	t.lmp = NewLayerMultiplex()
	// t.m = NewTileDb(cacheFile)
	// t.cacheFile = cacheFile
	t.url = url
	t.basedir = basedir
	os.Mkdir(t.basedir, 0755)
	t.m = make(map[string]*TileDb)

	return &t
}

/*
func (t *TileServer) AddXYZLayer(layerName string, url string) {
	t.lmp.AddRenderer(layerName, url)
}
*/

var pathRegex = regexp.MustCompile(`/([-A-Za-z0-9]+)/([0-9]+)/([0-9]+)/([0-9]+)(@[0-9]+x)?\.(png[0-9]{0,3}|jpe?g1?[0-9]{0,2})`)

func (t *TileServer) ServeTileRequest(w http.ResponseWriter, r *http.Request, tc TileCoord) {
	ch := make(chan TileFetchResult)

	tr := TileFetchRequest{tc, ch}
	k := fmt.Sprintf("%s_%s_%s", tc.Layer, tc.Scale, tc.Format)
	t.m[k].RequestQueue() <- tr

	result := <-ch
	needsInsert := false

	if result.BlobPNG == nil {
		// Tile was not provided by DB, so submit the tile request to the renderer
		t.lmp.SubmitRequest(tr)
		result = <-ch
		if result.BlobPNG == nil {
			// The tile could not be rendered, now we need to bail out.
			http.NotFound(w, r)
			return
		}
		needsInsert = true
	}

	w.Header().Set("Content-Type", "image/png")
	_, err := w.Write(result.BlobPNG)
	if err != nil {
		log.Println(err)
	}
	if needsInsert {
		t.m[k].InsertQueue() <- result // insert newly rendered tile into cache db
	}
}

func (t *TileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := pathRegex.FindStringSubmatch(r.URL.Path)

	if path == nil {
		http.NotFound(w, r)
		return
	}

	l := path[1]
	z, _ := strconv.ParseUint(path[2], 10, 64)
	x, _ := strconv.ParseUint(path[3], 10, 64)
	y, _ := strconv.ParseUint(path[4], 10, 64)
	scale := path[5]
	format := strings.Replace(path[6], "jpg", "jpeg", 1)

	k := fmt.Sprintf("%s_%s_%s", l, scale, format)
	_, p1 := t.m[k]
	if !p1 {
		t.m[k] = NewTileDb(fmt.Sprintf("%s/%s_%s_%s.mbtiles", t.basedir, l, scale, format))
	}
	_, present := t.lmp.layerChans[l]
	if !present {
		t.lmp.AddRenderer(l, t.url)
	}

	t.ServeTileRequest(w, r, TileCoord{x, y, z, t.TmsSchema, l, scale, t.url, format})
}
