package pool

import "errors"

//ErrPool is
type ErrPool error

//kinds of ErrPool
var (
	ErrTimeout               ErrPool = errors.New("ErrTimeout")
	ErrPoolClosed            ErrPool = errors.New("ErrPoolClosed")
	ErrConnectionPoolTimeout ErrPool = errors.New("ErrConnectionPoolTimeout")
	ErrMaxConnsReached       ErrPool = errors.New("ErrConnectionPoolTimeout")
	ErrMaxConnsParameter     ErrPool = errors.New("invalid maxConns parameter")
	ErrClosedConnectionPool  ErrPool = errors.New("ErrClosedConnectionPool")
	ErrConnectionPoolClosed  ErrPool = errors.New("ErrConnectionPoolClosed")
	ErrConnNotInPool         ErrPool = errors.New("ErrConnNotInPool")
	ErrNewConnFunc           ErrPool = errors.New("newConnFunc cannot be nil")
	ErrAddress               ErrPool = errors.New("invalid address settings")
	ErrMaximumParameter      ErrPool = errors.New("invalid maximum settings")
)
