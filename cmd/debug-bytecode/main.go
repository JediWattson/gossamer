package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: debug-bytecode <module.js> <function|#index|@hash>...")
		os.Exit(2)
	}
	source, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	module, err := compiler.CompileModuleWithOptions(string(source), compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		panic(err)
	}
	wanted := make(map[string]struct{}, len(os.Args)-2)
	wantedIndexes := make(map[int]struct{}, len(os.Args)-2)
	wantedHashes := make(map[uint64]struct{}, len(os.Args)-2)
	for _, name := range os.Args[2:] {
		if strings.HasPrefix(name, "#") {
			index, err := strconv.Atoi(strings.TrimPrefix(name, "#"))
			if err != nil {
				panic(fmt.Errorf("invalid Function selector %q: %w", name, err))
			}
			wantedIndexes[index] = struct{}{}
			continue
		}
		if strings.HasPrefix(name, "@") {
			hash, err := strconv.ParseUint(strings.TrimPrefix(name, "@"), 16, 64)
			if err != nil {
				panic(fmt.Errorf("invalid Function hash selector %q: %w", name, err))
			}
			wantedHashes[hash] = struct{}{}
			continue
		}
		wanted[name] = struct{}{}
	}
	image := module.Program()
	for index := 0; index < image.FunctionCount(); index++ {
		function, _ := image.Function(uint32(index))
		hash := fingerprint(function.Code)
		_, wantedName := wanted[function.Name]
		_, wantedIndex := wantedIndexes[index]
		_, wantedHash := wantedHashes[hash]
		if !wantedName && !wantedIndex && !wantedHash {
			continue
		}
		disassembly, err := browserruntime.Disassemble(function.Code)
		if err != nil {
			panic(err)
		}
		fmt.Printf(
			"FUNCTION %s index=%d hash=%016x instructions=%d constants=%d\n%s\n",
			function.Name, index, hash, len(function.Code)/browserruntime.InstructionWidth, len(function.Constants), disassembly,
		)
	}
}

func fingerprint(code []byte) uint64 {
	hash := uint64(14695981039346656037)
	for _, value := range code {
		hash ^= uint64(value)
		hash *= 1099511628211
	}
	return hash
}
