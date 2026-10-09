// Package sample is the fixture for unwired.Analyze. Every declaration here is
// labelled with what the scanner must say about it, so a wrong answer is a
// failing test rather than a judgement call.
package sample

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ReadField is read. reads=1
// WrittenOnlyField is assigned and never consulted. reads=0 writes=1
// NeverTouchedField is neither. reads=0 writes=0
// AccumulatedField is only ever `+=`, which consults the old value. reads>=1
type Config struct {
	ReadField         string
	WrittenOnlyField  int
	NeverTouchedField bool
	AccumulatedField  int
}

// UsedType is named by a parameter. reads>=1
type UsedType struct{ N int }

// UnusedType is named nowhere. reads=0
type UnusedType struct{ N int }

// UsedConst is read. reads=1
const UsedConst = "used"

// UnusedConst is read nowhere. reads=0
const UnusedConst = "unused"

// UsedVar is read. reads>=1
var UsedVar = 1

// UnusedVar is read nowhere. reads=0
var UnusedVar = 2

// Build populates a Config. It WRITES WrittenOnlyField and never reads it,
// which is the class whole-program reachability cannot see.
func Build() Config {
	c := Config{
		ReadField: UsedConst,
	}
	c.WrittenOnlyField = UsedVar
	c.AccumulatedField += 1
	return c
}

// Consume reads ReadField and AccumulatedField.
func Consume(c Config, u UsedType) string {
	_ = u.N
	_ = c.AccumulatedField
	return c.ReadField
}

// UsedFunc is called by Entry. reads=1
func UsedFunc() {}

// UnusedFunc is called nowhere. reads=0
func UnusedFunc() {}

// Method on UsedType, called by Entry. reads=1
func (u UsedType) Method() int { return u.N }

// UnusedMethod is called nowhere. reads=0
func (u UsedType) UnusedMethod() int { return u.N }

// Entry wires the live things together.
func Entry() {
	UsedFunc()
	c := Build()
	_ = Consume(c, UsedType{N: 1})
	_ = UsedType{}.Method()
}

// OnlyTestUsesThis is called only from sample_test.go. With the default
// settings that is reads=0; with TestsAsReads it is reads=1.
func OnlyTestUsesThis() int { return 1 }

// Handler is an interface with one method production code CALLS and one it does
// not. It is the shape every parser, plugin and handler in the estate has.
type Handler interface {
	// Handle is called through the interface by Dispatch. reads>=1
	Handle() int
	// Describe is called nowhere, by anyone. reads=0
	Describe() string
}

// Impl satisfies Handler.
type Impl struct{}

// Handle is reached only through Handler, never by name. It must NOT be
// reported: the call site names the interface method.
func (Impl) Handle() int { return 1 }

// Describe is reached by nothing, through the interface or otherwise, so it
// must still be reported. An interface method nobody calls does not make its
// implementations live.
func (Impl) Describe() string { return "" }

// Dispatch calls Handle through the interface.
func Dispatch(h Handler) int { return h.Handle() }

// WireHandler keeps Impl and Dispatch reachable.
func WireHandler() int { return Dispatch(Impl{}) }

// KeyConst is read as a map key in a composite literal. reads>=1, writes=0
const KeyConst = "key"

// KeyVar is read as a map key in a composite literal. reads>=1, writes=0
var KeyVar = "other"

// IndexConst is read as an array index in a composite literal. reads>=1, writes=0
const IndexConst = 1

// AssignedOnlyVar is assigned and read nowhere. reads=0, writes>=1
var AssignedOnlyVar int

// WireLiteralKeys uses each key above once.
func WireLiteralKeys() (map[string]int, [2]int) {
	AssignedOnlyVar = 7
	return map[string]int{KeyConst: 1, KeyVar: 2}, [2]int{IndexConst: 3}
}

// Wire is a JSON body. Name is read by encoding/json through its tag, which
// no Go read names: reads>=1 via reflection. Skip carries the tag "-", so the
// encoder never reads it: reads=0. Plain has no tag: reads=0.
type Wire struct {
	Name  string `json:"name"`
	Skip  string `json:"-"`
	Plain string
}

// Encode marshals a Wire.
func Encode() ([]byte, error) {
	w := Wire{Name: "n", Skip: "s", Plain: "p"}
	return json.Marshal(w)
}

// Buf is used as an io.Writer by fmt.Fprint, an interface of the standard
// library whose call site the scan cannot see. Write: reads>=1 via interface.
// Flushh matches no interface: reads=0.
type Buf struct{ n int }

func (b *Buf) Write(p []byte) (int, error) { b.n += len(p); return len(p), nil }

func (b *Buf) Flushh() {}

// Print writes through Buf.
func Print() int {
	b := &Buf{}
	fmt.Fprint(b, "x")
	return b.n
}

// LostWriter satisfies io.Writer, but production code names it nowhere, so its
// Write must stay unwired.
type LostWriter struct{}

func (LostWriter) Write(p []byte) (int, error) { return len(p), nil }

// Checker is a generic interface, and Checker[*Wire] is its instance.
// GenericImpl satisfies only the instance. Check is reached through it.
type Checker[T any] interface{ Check(T) bool }

// GenericImpl checks a Wire.
type GenericImpl struct{}

func (GenericImpl) Check(w *Wire) bool { return w.Name != "" }

// RunCheck calls Check through the instance.
func RunCheck(c Checker[*Wire]) bool { return c.Check(&Wire{}) }

// WireCheck keeps GenericImpl reachable.
func WireCheck() bool { return RunCheck(GenericImpl{}) }

// WrapErr wraps an error. errors.Is reaches Unwrap through an anonymous
// interface: reads>=1.
type WrapErr struct{ err error }

func (e *WrapErr) Error() string { return "wrap" }

func (e *WrapErr) Unwrap() error { return e.err }

// IsWrapped asks errors.Is about a WrapErr.
func IsWrapped(target error) bool { return errors.Is(&WrapErr{}, target) }

// OnlyConsumerUses is called by the consumer module only. Alone: reads=0.
// With the consumer loaded: reads>=1.
func OnlyConsumerUses() int { return 3 }

// OnlyConsumerTestUses is called by a test of the consumer only: reads=0
// either way.
func OnlyConsumerTestUses() int { return 4 }
