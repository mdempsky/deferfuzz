# deferfuzz

deferfuzz is a fuzzer for Go defer/panic/recover.

Caveat: I wrote this in a couple hours, and I'm more of a compiler
engineer than a fuzzer engineer. The generated test cases are
terrible, but not too difficult to minimize.

Trophies:

* https://golang.org/issue/43920
* https://golang.org/issue/43921
