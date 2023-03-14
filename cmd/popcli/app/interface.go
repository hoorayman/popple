package app

type Client interface {
	FetchKey(key string) (string, error)
	SetKey(key, val string) (string, error)
	DelKey(key string) (string, error)
}
