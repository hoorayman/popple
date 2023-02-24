package statemachine

type IKVDB interface {
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Call(command []byte) error
	CommandCheck(command []byte) bool
}
