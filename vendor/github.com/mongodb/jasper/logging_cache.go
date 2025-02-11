package jasper

import (
	"context"
	"sync"
	"time"

	"github.com/mongodb/grip"
	"github.com/mongodb/jasper/options"
	"github.com/pkg/errors"
)

// LoggingCache provides an interface to a cache of loggers.
// TODO (EVG-13100): most of these methods should return errors.
type LoggingCache interface {
	// Create creates and caches a new logger based on the given output options.
	Create(id string, opts *options.Output) (*options.CachedLogger, error)
	// Put adds an existing logger to the cache.
	Put(id string, logger *options.CachedLogger) error
	// Get gets an existing cached logger. Implementations should return nil if
	// the logger cannot be found.
	Get(id string) *options.CachedLogger
	// Remove removes an existing logger from the logging cache.
	Remove(id string)
	// CloseAndRemove closes and removes an existing logger from the
	// logging cache.
	CloseAndRemove(ctx context.Context, id string) error
	// Clear closes and removes any remaining loggers in the logging cache.
	Clear(ctx context.Context) error
	// Prune removes all loggers that were last accessed before the given
	// timestamp.
	Prune(lastAccessed time.Time)
	// Len returns the number of loggers. Implementations should return
	// -1 if the length cannot be retrieved successfully.
	Len() int
}

// NewLoggingCache produces a thread-safe implementation of a local logging
// cache for use in manager implementations.
func NewLoggingCache() LoggingCache {
	return &loggingCacheImpl{
		cache: map[string]*options.CachedLogger{},
	}
}

type loggingCacheImpl struct {
	cache map[string]*options.CachedLogger
	mu    sync.RWMutex
}

func (c *loggingCacheImpl) Create(id string, opts *options.Output) (*options.CachedLogger, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cache[id]; ok {
		return nil, errors.Errorf("logger named %s exists", id)
	}
	logger := opts.CachedLogger(id)

	c.cache[id] = logger

	return logger, nil
}

func (c *loggingCacheImpl) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.cache)
}

func (c *loggingCacheImpl) Prune(ts time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.cache {
		if v.Accessed.Before(ts) {
			delete(c.cache, k)
		}
	}
}

func (c *loggingCacheImpl) Get(id string) *options.CachedLogger {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cache[id]; !ok {
		return nil
	}

	item := c.cache[id]
	item.Accessed = time.Now()
	c.cache[id] = item
	return item
}

func (c *loggingCacheImpl) Put(id string, logger *options.CachedLogger) error {
	if logger == nil {
		return errors.New("cannot cache nil logger")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cache[id]; ok {
		return errors.Errorf("cannot cache with existing logger '%s'", id)
	}

	logger.Accessed = time.Now()

	c.cache[id] = logger

	return nil
}

func (c *loggingCacheImpl) Remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, id)
}

func (c *loggingCacheImpl) CloseAndRemove(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var err error
	logger, ok := c.cache[id]
	if ok {
		err = logger.Close()
		delete(c.cache, id)
	}

	return errors.Wrapf(err, "problem closing logger with id %s", id)
}

func (c *loggingCacheImpl) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	catcher := grip.NewBasicCatcher()
	for _, logger := range c.cache {
		catcher.Add(logger.Close())
	}
	c.cache = map[string]*options.CachedLogger{}

	return errors.Wrap(catcher.Resolve(), "problem clearing logger cache")
}
