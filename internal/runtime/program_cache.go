package runtime

import "bytes"

const defaultBytecodeCacheLimit = 4 * 1024

type bytecodeCacheKey struct {
	hash          uint64
	bytes         int
	constantCount int
}

type cachedBytecode struct {
	key          bytecodeCacheKey
	code         []byte
	instructions []Instruction
	referenced   bool
}

// decodeProgram decodes and verifies an immutable Function body once. Closure
// instances frequently share the same code while carrying different captured
// environments, so the cache is content-addressed rather than Ref-addressed.
func (interpreter *Interpreter) decodeProgram(code []byte, constantCount int) ([]Instruction, error) {
	key := bytecodeKey(code, constantCount)
	interpreter.programMutex.Lock()
	if cached := interpreter.cachedProgramLocked(key, code); cached != nil {
		cached.referenced = true
		instructions := cached.instructions
		interpreter.programMutex.Unlock()
		return instructions, nil
	}
	interpreter.programMutex.Unlock()

	instructions, err := DecodeBytecode(code)
	if err != nil {
		return nil, err
	}
	if err := verifyInstructions(instructions, constantCount); err != nil {
		return nil, err
	}

	interpreter.programMutex.Lock()
	defer interpreter.programMutex.Unlock()
	if cached := interpreter.cachedProgramLocked(key, code); cached != nil {
		cached.referenced = true
		return cached.instructions, nil
	}
	cached := &cachedBytecode{
		key:          key,
		code:         append([]byte(nil), code...),
		instructions: instructions,
		referenced:   true,
	}
	interpreter.admitProgramLocked(cached)
	return cached.instructions, nil
}

func (interpreter *Interpreter) cachedProgramLocked(key bytecodeCacheKey, code []byte) *cachedBytecode {
	for _, cached := range interpreter.programs[key] {
		if bytes.Equal(cached.code, code) {
			return cached
		}
	}
	return nil
}

func (interpreter *Interpreter) admitProgramLocked(cached *cachedBytecode) {
	if len(interpreter.programClock) < defaultBytecodeCacheLimit {
		interpreter.programClock = append(interpreter.programClock, cached)
	} else {
		for interpreter.programClock[interpreter.programAt].referenced {
			interpreter.programClock[interpreter.programAt].referenced = false
			interpreter.programAt = (interpreter.programAt + 1) % len(interpreter.programClock)
		}
		expired := interpreter.programClock[interpreter.programAt]
		bucket := interpreter.programs[expired.key]
		for index, candidate := range bucket {
			if candidate != expired {
				continue
			}
			bucket[index] = bucket[len(bucket)-1]
			bucket = bucket[:len(bucket)-1]
			break
		}
		if len(bucket) == 0 {
			delete(interpreter.programs, expired.key)
		} else {
			interpreter.programs[expired.key] = bucket
		}
		interpreter.programClock[interpreter.programAt] = cached
		interpreter.programAt = (interpreter.programAt + 1) % len(interpreter.programClock)
	}
	interpreter.programs[cached.key] = append(interpreter.programs[cached.key], cached)
}

func bytecodeKey(code []byte, constantCount int) bytecodeCacheKey {
	// FNV-1a is only the bucket index. bytes.Equal above preserves correctness
	// under collisions.
	hash := uint64(14695981039346656037)
	for _, value := range code {
		hash ^= uint64(value)
		hash *= 1099511628211
	}
	return bytecodeCacheKey{hash: hash, bytes: len(code), constantCount: constantCount}
}
