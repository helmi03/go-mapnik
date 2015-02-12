package maptiles

import "log"

type LayerMultiplex struct {
	layerChans map[string]chan<- TileFetchRequest
}

func NewLayerMultiplex() *LayerMultiplex {
	l := LayerMultiplex{}
	l.layerChans = make(map[string]chan<- TileFetchRequest)
	return &l
}

func DefaultRenderMultiplex(defaultStylesheet string) *LayerMultiplex {
	l := NewLayerMultiplex()
	c := NewTileRendererChan(defaultStylesheet)
	l.layerChans[""] = c
	l.layerChans["default"] = c
	return l
}

func (l *LayerMultiplex) AddRenderer(name string, url string) {
	name = "default"
	l.layerChans[name] = NewTileRendererChan(url)
}

func (l *LayerMultiplex) AddSource(name string, fetchChan chan<- TileFetchRequest) {
	name = "default"
	l.layerChans[name] = fetchChan
}

func (l LayerMultiplex) SubmitRequest(r TileFetchRequest) bool {
	// name = r.Coord.Layer
	name := "default"
	c, ok := l.layerChans[name]
	if ok {
		c <- r
	} else {
		log.Println("No such layer", r.Coord.Layer)
	}
	return ok
}
