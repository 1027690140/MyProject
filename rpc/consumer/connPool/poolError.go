package pool

import "errors"

//ErrPool is
type ErrPool error

//kinds of ErrPool
var (
	ErrTimeout                ErrPool = errors.New("Err Timeout")
	ErrPoolClosed             ErrPool = errors.New("Err Pool Closed")
	ErrGetConnectionTimeout   ErrPool = errors.New("Err Get Connection Timeout")
	ErrMaxConnsReached        ErrPool = errors.New("Err Max Conns Reached")
	ErrMaxConnsParameter      ErrPool = errors.New("invalid maxConns parameter")
	ErrClosedConnectionPool   ErrPool = errors.New("Err Closed ConnectionPool")
	ErrConnectionPoolClosed   ErrPool = errors.New("Err Connection Pool Closed")
	ErrConnNotInPool          ErrPool = errors.New("Err Conn Not In Pool")
	ErrNewConnFunc            ErrPool = errors.New("newConnFunc cannot be nil")
	ErrAddress                ErrPool = errors.New("invalid address settings")
	ErrMaximumParameter       ErrPool = errors.New("invalid maximum settings")
	ErrPoolNotExist           ErrPool = errors.New("pool not exist")
	ErrPoolsParameterNotExist ErrPool = errors.New("new Pools Parameter Not Exist")
	ErrPoolsOptionNotExist    ErrPool = errors.New("new Pools Option Not Exist")
	ErrPoolEmpty              ErrPool = errors.New("new Pools Empty  ")
	ErrPoolFull               ErrPool = errors.New("new Pools Full   ")
	ErrShrinkTooMuch          ErrPool = errors.New("Shrink too often")
	ErrExpandTooMuch          ErrPool = errors.New("Expand too often")
)
