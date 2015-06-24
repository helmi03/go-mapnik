package maptiles

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/groupcache"
)

var (
	pool  *groupcache.HTTPPool
	cache *groupcache.Group
)

// TODO serve list of registered layers per HTTP (preferably leafletjs-compatible js-array)

// Handles HTTP requests for map tiles, caching any produced tiles
// in an MBtiles 1.2 compatible sqlite db.
type TileServer struct {
	m         map[string]*TileDb
	lmp       *LayerMultiplex
	TmsSchema bool
	// cacheFile string
	url       string
	basedir   string
	cache     *groupcache.Group
	PathComps map[string]string
}

func NewTileServer(url, basedir string) *TileServer {
	t := TileServer{}
	t.lmp = NewLayerMultiplex()
	t.url = url
	t.basedir = basedir
	os.Mkdir(t.basedir, 0755)
	t.m = make(map[string]*TileDb)
	pool = groupcache.NewHTTPPool(fmt.Sprintf("http://127.0.0.1:%v", 9999))
	t.cache = groupcache.NewGroup("TileCache", 100*1048576, groupcache.GetterFunc(
		func(ctx groupcache.Context, key string, dest groupcache.Sink) error {
			fnParams := strings.Split(key, ":")
			pathParams := strings.Split(fnParams[0], "/")
			z, _ := strconv.ParseUint(pathParams[0], 0, 64)
			x, _ := strconv.ParseUint(pathParams[1], 0, 64)
			y, _ := strconv.ParseUint(pathParams[2], 0, 64)
			fn := fnParams[1]
			//flip y to match TMS spec
			y = (1 << z) - 1 - y

			var tileData []byte

			db, err := sql.Open("sqlite3", fn)
			if err != nil {
				fmt.Printf("Error database. %s\n", err.Error())
			}
			defer db.Close()
			queryString := `
			SELECT tile_data
			FROM tile_blobs
			WHERE checksum=(
				SELECT checksum
				FROM layered_tiles
				WHERE zoom_level=?
				AND tile_column=?
				AND tile_row=?
				AND layer_id='0')`
			row := db.QueryRow(queryString, z, x, y)
			row.Scan(&tileData)
			dest.SetBytes(tileData)
			return nil
		}))

	return &t
}

func (t *TileServer) ServeTileRequest(w http.ResponseWriter, r *http.Request, tc TileCoord) {

	ch := make(chan TileFetchResult)

	tr := TileFetchRequest{tc, ch}
	k := fmt.Sprintf("%s_%s_%s", tc.Layer, tc.Scale, tc.Format)
	t.m[k].RequestQueue() <- tr

	result := <-ch
	needsInsert := false

	if result.Blob == nil {
		// Tile was not provided by DB, so submit the tile request to the renderer
		t.lmp.SubmitRequest(tr)
		result = <-ch
		if result.Blob == nil {
			// The tile could not be rendered, now we need to bail out.
			http.NotFound(w, r)
			return
		}
		needsInsert = true
	}

	if tc.Format == "vector.pbf" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/x-protobuf")
		// mapbox-gl-js need content-encoding=gzip
		// Mapbox Studio tmsource don't like content-encoding=gzip
		gz := r.FormValue("gz")
		if v, ok := r.Form["gz"]; ok {
			if false {
				fmt.Println("Found", v, gz)
			}
			w.Header().Set("Content-Encoding", "gzip")
		}
	} else {
		w.Header().Set("Content-Type", "image/png")
	}
	etag := fmt.Sprintf("%x", md5.Sum(result.Blob))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if len(result.Blob) > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", int64(len(result.Blob))))
	}
	w.Header().Del("Vary")
	w.Header().Add("ETag", etag)
	_, err := w.Write(result.Blob)
	if err != nil {
		log.Println(err)
	}
	if needsInsert {
		t.m[k].InsertQueue() <- result // insert newly rendered tile into cache db
	}
}

func (t *TileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var z, x, y uint64
	var l, format, scale string
	if len(t.PathComps) > 0 {
		z, _ = strconv.ParseUint(t.PathComps["z"], 10, 64)
		x, _ = strconv.ParseUint(t.PathComps["x"], 10, 64)
		y, _ = strconv.ParseUint(t.PathComps["y"], 10, 64)
		l = t.PathComps["layername"]
		format = t.PathComps["format"]
		scale = t.PathComps["scale"]
	} else {
		pathRegex := regexp.MustCompile(`/([-A-Za-z0-9]+)/([0-9]+)/([0-9]+)/([0-9]+)(@[0-9]+x)?\.(png[0-9]{0,3}|jpe?g1?[0-9]{0,2}|(vector\.)?pbf)`)
		path := pathRegex.FindStringSubmatch(r.URL.Path)

		if path == nil {
			http.NotFound(w, r)
			return
		}

		l = path[1]
		z, _ = strconv.ParseUint(path[2], 10, 64)
		x, _ = strconv.ParseUint(path[3], 10, 64)
		y, _ = strconv.ParseUint(path[4], 10, 64)
		scale = path[5]
		format = path[6]
	}
	format = strings.Replace(format, "jpg", "jpeg", 1)
	if format == "pbf" {
		format = "vector.pbf"
	}

	k := fmt.Sprintf("%s_%s_%s", l, scale, format)
	_, p1 := t.m[k]
	var fn string
	if !p1 {
		fn = fmt.Sprintf("%s/%s_%s.mbtiles", t.basedir, l, format)
		if scale != "" {
			fn = fmt.Sprintf("%s/%s_%s_%s.mbtiles", t.basedir, l, scale, format)
		}
		t.m[k] = NewTileDb(fn)
		t.m[k].path = fn
	}
	_, present := t.lmp.layerChans[l]
	if !present {
		t.lmp.AddRenderer(l, t.url)
	}
	url := t.url
	if format == "vector.pbf" {
		url = strings.Replace(url, "tmstyle", "tmsource", 1)
		url = strings.Replace(url, ".tm2", ".tm2source", 1)
		url = strings.Replace(url, "/style", "/source", 1)
	}

	var data []byte
	key := fmt.Sprintf("%d/%d/%d:%s", z, x, y, t.m[k].path)
	err := t.cache.Get(nil, key, groupcache.AllocatingByteSliceSink(&data))
	if err != nil {
		log.Printf("Error groupcache. %s\n", err.Error())
	}
	if len(data) > 1 {
		etag := fmt.Sprintf("%x", md5.Sum(data))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Add("ETag", etag)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", int64(len(data))))
		w.Header().Del("Vary")
		_, err := w.Write(data)
		if err != nil {
			log.Printf("Error write to response, %s", err.Error())
		}
		return
	}

	t.ServeTileRequest(w, r, TileCoord{x, y, z, t.TmsSchema, l, scale, url, format})
}
