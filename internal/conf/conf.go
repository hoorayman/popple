// Package conf provide config
package conf

import (
	"log"

	"time"

	"github.com/go-playground/validator/v10"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var c *Conf

func init() {
	c = NewDefault()
}

type Option func(*Conf)

func SetConfigFile(file string) Option {
	return func(c *Conf) {
		if file != "" {
			c.file = file
			c.SetConfigFile(c.file)
			err := c.ReadInConfig()
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func SetEnv() Option {
	return func(c *Conf) {
		c.AutomaticEnv()
	}
}

type Conf struct {
	*viper.Viper
	file     string
	validate *validator.Validate
}

func NewDefault() *Conf {
	return New(viper.GetViper())
}

func New(v *viper.Viper, opts ...Option) *Conf {
	c := &Conf{
		Viper:    v,
		validate: validator.New(),
	}

	for _, o := range opts {
		o(c)
	}

	return c
}

func GetRaftPort() int {
	return 0
}

func SetDefault(key string, value interface{}) { c.SetDefault(key, value) }

func Set(key string, value interface{}) { c.Set(key, value) }

func Get(key string) interface{} { return c.Get(key) }

func GetString(key string) string { return c.GetString(key) }

func GetBool(key string) bool { return c.GetBool(key) }

func GetInt(key string) int { return c.GetInt(key) }

func GetUint32(key string) uint32 { return c.GetUint32(key) }

func GetInt32(key string) int32 { return c.GetInt32(key) }

func GetInt64(key string) int64 { return c.GetInt64(key) }

func GetFloat64(key string) float64 { return c.GetFloat64(key) }

func GetDuration(key string) time.Duration {
	return c.GetDuration(key)
}

// BindPFlag bind pflag
func BindPFlag(key string, flag *pflag.Flag) error { return c.BindPFlag(key, flag) }

// InitConfig set options to config
func InitConfig(opts ...Option) {
	for _, opt := range opts {
		opt(c)
	}
}
