package types

type HousekeepingJob interface {
	Name() string
	First() error
	Sometimes() error
	Last() error
}
