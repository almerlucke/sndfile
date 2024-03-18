package backend

type Backend interface {
	Close() error
	Normalize(float32) error
	Write([]float32) error
}
