package surf

type URLPath struct {
	path string
}

func Path(path string) *URLPath {
	return &URLPath{
		path: path,
	}
}

func (p *URLPath) LastChunk() string {
	path := p.path

	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]

	}

	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
