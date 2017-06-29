// Package spies provides test spies for go test, in the vien of testify's mocks.
// Roughly speaking, define a spy like this:
//  type MySpy {
//  	*spies.Spy
//  }
//
//  func NewMySpy() *MySpy {
//    return &MySpy{ NewSpy() }
//  }
//
//  func (my *MySpy) InterfaceMethod(with, some, args int) (ret string, err error) {
//    res := my.Called(with, some, args)
//    return res.String(0), res.Error(1)
//  }
// Use your spy in tests like:
//   my.MatchMethod("InterfaceMethod", spies.AnyArgs, "called", nil)
// ... which will return "called" with a nil error whenever InterfaceMethod is called.
// Several calls to Match and MatchMethod can be made in a row - the first match wins.
// Then you can check calls by calling my.Calls to my.CallsTo
package spies

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
)

type (
	matcher struct {
		pred   func(string, mock.Arguments) bool
		result mock.Arguments
	}

	call struct {
		method string
		args   mock.Arguments
		res    mock.Arguments
	}

	// A Spy is a type for use in testing - it's intended to be embedded in spy
	// implementations.
	Spy struct {
		matchers []matcher
		calls    []call
	}
)

// NewSpy makes a Spy
func NewSpy() *Spy {
	return &Spy{
		matchers: []matcher{},
		calls:    []call{},
	}
}

// Always is an always-true predicate
func Always(string, mock.Arguments) bool {
	return true
}

// AnyArgs is an always-true predicate for MethodMatch
func AnyArgs(mock.Arguments) bool {
	return true
}

func (s *Spy) String() string {
	str := "Calls: "
	for _, c := range s.calls {
		str += c.String() + "\n"
	}
	return str
}

func (c call) String() string {
	return fmt.Sprintf("%s(%s) -> (%s)", c.method, c.args, c.res)
}

// Match records an arbitrary predicate to match against a method call
func (s *Spy) Match(pred func(string, mock.Arguments) bool, result ...interface{}) {
	s.matchers = append(s.matchers, matcher{pred: pred, result: mock.Arguments(result)})
}

// MatchMethod records a predicate limited to a specific method name
func (s *Spy) MatchMethod(method string, pred func(mock.Arguments) bool, result ...interface{}) {
	s.matchers = append(s.matchers, matcher{
		pred: func(m string, as mock.Arguments) bool {
			if m != method {
				return false
			}
			return pred(as)
		},
		result: mock.Arguments(result),
	})
}

// Any records that any call to method get result as a reply
func (s *Spy) Any(method string, result ...interface{}) {
	s.matchers = append(s.matchers,
		matcher{
			pred: func(m string, a mock.Arguments) bool {
				return method == m
			},
			result: mock.Arguments(result),
		})
}

func (s *Spy) findArgs(functionName string, args mock.Arguments) mock.Arguments {
	for _, m := range s.matchers {
		if m.pred(functionName, args) {
			return m.result
		}
	}
	return nil
}

// Called is used by embedders of Spy to indicate that the method is called.
func (s *Spy) Called(argList ...interface{}) mock.Arguments {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("Couldn't get caller info")
	}

	functionPath := runtime.FuncForPC(pc).Name()
	//Next four lines are required to use GCCGO function naming conventions.
	//For Ex:  github_com_docker_libkv_store_mock.WatchTree.pN39_github_com_docker_libkv_store_mock.Mock
	//uses interface information unlike golang github.com/docker/libkv/store/mock.(*Mock).WatchTree
	//With GCCGO we need to remove interface information starting from pN<dd>.
	re := regexp.MustCompile(`\.pN\d+_`)
	if re.MatchString(functionPath) {
		functionPath = re.Split(functionPath, -1)[0]
	}
	parts := strings.Split(functionPath, ".")
	functionName := parts[len(parts)-1]

	args := mock.Arguments(argList)

	found := s.findArgs(functionName, args)

	if found == nil {
		panic(errors.Errorf("Couldn't find an expected call for %s(%s)", functionName, args))
	}

	s.calls = append(s.calls, call{functionName, args, found})
	return found
}

// CallsTo returns the calls to the named method
func (s *Spy) CallsTo(name string) []call {
	calls := []call{}
	for _, c := range s.calls {
		if c.method == name {
			calls = append(calls, c)
		}
	}
	return calls
}

// Calls returns all the calls made to the spy
func (s *Spy) Calls() []call {
	cs := make([]call, len(s.calls))
	copy(cs, s.calls)
	return cs
}
