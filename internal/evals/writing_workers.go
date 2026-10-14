package evals

import (
	"context"
	"errors"
	"sync"
)

func runWritingJobs[T any](ctx context.Context, concurrency int, jobs []T, fn func(context.Context, T) error) error {
	if concurrency < 1 {
		return errors.New("writing concurrency must be positive")
	}
	jobCh := make(chan T)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			consumeWritingJobs(workerCtx, jobCh, fn, func(err error) { reportWritingJobFailure(errCh, cancel, err) })
		}()
	}
	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-workerCtx.Done():
			close(jobCh)
			workers.Wait()
			select {
			case runErr := <-errCh:
				return runErr
			default:
				return workerCtx.Err()
			}
		}
	}
	close(jobCh)
	workers.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func consumeWritingJobs[T any](workerCtx context.Context, jobCh <-chan T, fn func(context.Context, T) error, onError func(error)) {
	for job := range jobCh {
		if workerCtx.Err() != nil {
			return
		}
		if err := fn(workerCtx, job); err != nil {
			onError(err)
			return
		}
	}
}

func reportWritingJobFailure(errCh chan<- error, cancel context.CancelFunc, err error) {
	select {
	case errCh <- err:
		cancel()
	default:
	}
}
