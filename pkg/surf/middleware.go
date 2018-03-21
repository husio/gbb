package surf

// Middleware is a function that takes handler in one of recognized by surf
// notations and returns new handler, wrapping original one with additional
// functionality.
type Middleware func(handler interface{}) Handler

// WithMiddlewares attach given collection of middlewares. First one is called last.
func WithMiddlewares(handler interface{}, middlewares []Middleware) Handler {
	h := AsHandler(handler)
	// apply in reverse order
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
