package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os/exec"
	"time"
)

// Fuzzer design notes:
//
// There are three kinds of defers:
// 1. Heap allocated: defers that occur inside a loop. (E.g., "for { defer f(); break }" suffices.)
// 2. Stack allocated: defers that occur outside of a block.
// 3. Open-coded defers: stack allocated defers that occur in a function that can be open-coded.
//
// 1 and 2 can mix and interleave within a single function.
// 3 always occurs by itself.
//
// A function is "progressing" or "unwinding".
// When panicking, it either has an associated panic ("panicking") or not.
//
// While progressing, if a function call finishes with a panic,
// the function switches to "unwinding" with an associated panic.
//
// If the function reaches the end of its block or a "return" statement,
// it switches to "unwinding" without an associated panic.
//
// Unwinding means running all deferred functions in reverse order.
// If a deferred function calls recover,
// it returns the associated panic (if any) or nil (if none)
// and disassociates from it.
//
//
// Plan: Generate a random call tree. Have, say, 100 steps at most.
// Each step can be a regular or deferred call.
// Each call can be of "assert", "panic", "recover", or a function literal.
// Every function literal contains "type _ int" to prevent it from being inlined.
//
// After generating the call tree, we simulate the calls,
// and calculate the order the asserts are expected to execute in.
// The generated code then is actually "assert(N)",
// and there's a runtime support function "func assert(n int)"
// to validate that the sequence occured correctly.
//
// Perhaps recovers should be numbered too,
// with the panic value they're expecting to receive?
//
// I think "func main()" should always start with "defer func() { recover() }()".

func main() {
	rand.Seed(time.Now().UnixNano())

	for i := 0; ; i++ {
		fmt.Println(i)

		buf := generate()
		ioutil.WriteFile("test.go", buf, 0666)
		if err := exec.Command("go", "run", "test.go").Run(); err != nil {
			log.Fatal("hm?", err)
		}
	}
}

func generate() []byte {
	steps, panics = 0, 0

	var m Multi
	m.Body = []*Stmt{{Defer: true, Call: &Multi{Body: []*Stmt{{Call: &Unit{Kind: Recover}}}}}}

	f := Fuzzer{budget: 100}
	f.Fill(&m)

	var a int
	b := Run(&m, &a)
	if a != 0 || b != 0 {
		log.Fatalf("huh? %v %v", a, b)
	}

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "package main; import `log`; func main() {")
	Write(&buf, &m)
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf, `

func expect(n int, err interface{}) {
	println("expect", n)
	if n != err && !(n == 0 && err == nil) {
		log.Fatalf("have %v, want %v", err, n)
	}
}

var steps int

func step(want int) {
	println("step", want)
	steps++
	if steps != want {
		log.Fatalf("have %v, want %v", steps, want)
	}
}
`)

	out, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	return out
}

var steps, panics int

func Run(m *Multi, outer *int) int {
	panic := 0
	var defers []interface{}

	call := func(c interface{}, panicp *int) {
		switch c := c.(type) {
		case *Unit:
			switch c.Kind {
			case Normal:
				steps++
				c.N = steps
			case Panic:
				panics++
				c.N = panics
				panic = panics
			case Recover:
				c.N = *outer
				*outer = 0
			}
		case *Multi:
			if n := Run(c, panicp); n != 0 {
				panic = n
			}
		}
	}

	for _, stmt := range m.Body {
		if stmt.Defer {
			defers = append(defers, stmt.Call)
			continue
		}
		call(stmt.Call, new(int))
		if panic != 0 {
			break
		}
	}

	for i := len(defers) - 1; i >= 0; i-- {
		call(defers[i], &panic)
	}

	return panic
}

func Write(w io.Writer, m *Multi) {
	fmt.Fprintln(w, "type _ int") // prevent inlining
	for _, stmt := range m.Body {
		if stmt.Defer {
			fmt.Fprint(w, "defer ")
		}

		switch call := stmt.Call.(type) {
		case *Unit:
			switch call.Kind {
			case Normal:
				fmt.Fprintf(w, "step(%v)\n", call.N)
			case Panic:
				fmt.Fprintf(w, "panic(%v)\n", call.N)
			case Recover:
				if stmt.Defer {
					log.Fatal("defer of expect(recover()) doesnt make sense")
				}
				fmt.Fprintf(w, "expect(%v, recover())\n", call.N)
			}

		case *Multi:
			fmt.Fprintln(w, "func() {")
			Write(w, call)
			fmt.Fprintln(w, "}()")
		}
	}
}

type Fuzzer struct {
	budget int
}

func (f *Fuzzer) Fill(m *Multi) {
	for f.budget > 0 {
		Defer := false
		if rand.Intn(2) == 0 {
			Defer = true
		}

		var call interface{}
		var waspanic bool
		switch rand.Intn(10) {
		case 0, 4, 5, 6:
			b2 := rand.Intn(f.budget)
			f.budget -= b2
			m2 := new(Multi)
			f2 := &Fuzzer{budget: b2}
			f2.Fill(m2)
			f.budget += f2.budget
			call = m2
		case 2, 7, 8:
			if !Defer {
				call = &Unit{Kind: Recover, N: -1}
				f.budget--
				break
			}
			fallthrough
		case 1, 9:
			call = &Unit{Kind: Normal, N: -1}
			f.budget--
		case 3:
			call = &Unit{Kind: Panic, N: -1}
			f.budget--
			waspanic = true
		}
		m.Body = append(m.Body, &Stmt{Defer: Defer, Call: call})
		if waspanic && !Defer {
			break
		}
	}
}

type Stmt struct {
	Defer bool
	Call  interface{} // *Unit or *Multi
}

type Kind int

const (
	Normal Kind = iota
	Recover
	Panic
)

type Unit struct {
	Kind Kind
	N    int
}

type Multi struct {
	Body []*Stmt
}
