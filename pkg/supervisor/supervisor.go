//  Copyright (C) 2021-2023 Chronicle Labs, Inc.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package supervisor

import (
	"context"
	"errors"
	"reflect"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/log/null"
)

const LoggerTag = "SUPERVISOR"

// Service that could be managed by Supervisor.
type Service interface {
	// Start starts the service.
	Start(ctx context.Context) error

	// Wait returns a channel that is blocked while service is running.
	// When the service is stopped, the channel will be closed. If an error
	// occurs, an error will be sent to the channel before closing it.
	Wait() <-chan error
}

// Supervisor manages long-running services that implement the Service
// interface. If any of the managed services fail, all other services are
// stopped. This ensures that all services are running or none.
type Supervisor struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	waitCh    chan error
	services  []Service
	log       log.Logger
}

// New returns a new instance of *Supervisor.
func New(logger log.Logger) *Supervisor {
	if logger == nil {
		logger = null.New()
	}
	return &Supervisor{
		waitCh: make(chan error),
		log:    logger.WithField("tag", LoggerTag),
	}
}

// Watch add one or more services to a supervisor. Services must be added
// before invoking the Start method, otherwise it panics.
func (s *Supervisor) Watch(services ...Service) {
	if s.ctx != nil {
		s.log.Panic("supervisor was already started")
	}
	s.services = append(s.services, services...)
}

// Start starts all watched services. It can be invoked only once, otherwise
// it panics.
func (s *Supervisor) Start(ctx context.Context) error {
	if s.ctx != nil {
		return errors.New("service can be started only once")
	}
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	s.ctx, s.ctxCancel = context.WithCancel(ctx)
	for _, srv := range s.services {
		s.log.
			WithField("service", serviceName(srv)).
			Debug("Starting service")
		if err := srv.Start(s.ctx); err != nil {
			s.ctxCancel()
			close(s.waitCh)
			return err
		}
	}
	go s.serviceMonitor()
	return nil
}

// Wait returns a channel that is blocked until at least one service is
// running. When all services are stopped, the channel will be closed.
// If an error occurs in any of the services, it will be sent to the
// channel before closing it. If multiple service crash, only the first
// error is returned.
func (s *Supervisor) Wait() <-chan error {
	return s.waitCh
}

func (s *Supervisor) serviceMonitor() {
	var err error
	// In this loop, a select is created (using reflection) that waits until
	// at least one service has completed its work. This is reported by
	// closing the channel returned by the Wait() or returning an error from
	// the same channel (see the Service interface). The service is then
	// removed from the s.service list and the loop is executed again until
	// no service remains.
	for len(s.services) > 0 {
		// Wait for first stopped service:
		c := make([]reflect.SelectCase, len(s.services))
		for i, srv := range s.services {
			c[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(srv.Wait())}
		}
		n, v, ok := reflect.Select(c)
		name := serviceName(s.services[n])

		// If service failed, cancel the context to stop the others:
		if !v.IsNil() {
			s.log.
				WithError(v.Interface().(error)).
				WithField("service", name).
				Error("Service crashed")
			if err == nil {
				err = v.Interface().(error) // TODO(mdobak): Consider using multierror.
			}
			s.ctxCancel()
			continue
		}

		s.log.
			WithField("service", name).
			Debug("Service stopped")

		// Remove service from list if channel is closed:
		// TODO(mdobak): If service is not removed from the list, there is no need
		//               rebuild select cases above.
		if !ok {
			s.services = append(s.services[:n], s.services[n+1:]...)
		}
	}
	if err != nil {
		s.waitCh <- err
	}
	close(s.waitCh)
}

func serviceName(s interface{}) string {
	return reflect.Indirect(reflect.ValueOf(s)).Type().String()
}
