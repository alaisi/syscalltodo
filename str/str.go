package str

func Itoa(num int) string {
	return Ltoa(int64(num))
}
func Ltoa(num int64) string {
	if num == 0 {
		return "0"
	}
	abs, len, negative := num, 0, num < 0
	if negative {
		len = 1
		abs *= -1
	}
	for n := abs; n != 0; n /= 10 {
		len++
	}
	bytes := make([]byte, len)
	for i, n := len-1, abs; n != 0; n /= 10 {
		bytes[i] = byte(n%10) + '0'
		i--
	}
	if negative {
		bytes[0] = '-'
	}
	return string(bytes)
}

func Atoi(s string) int {
	return int(Atol(s))
}
func Atol(s string) int64 {
	start, negative := 0, len(s) > 0 && s[0] == '-'
	if negative {
		start = 1
	}
	num := int64(0)
	for i := start; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		num = num*10 + int64(c-'0')
	}
	if negative {
		num *= -1
	}
	return num
}

func Split(str string, c rune) []string {
	parts := make([]string, 0, 1)
	start, i := 0, 0
	var s rune
	for i, s = range str {
		if s == c {
			parts = append(parts, str[start:i])
			start = i + 1
		}
	}
	parts = append(parts, str[start:])
	return parts
}

func IndexOf(str string, c rune) int {
	for i, s := range str {
		if s == c {
			return i
		}
	}
	return -1
}

func IndexOfString(str string, str2 string) int {
	size := len(str2)
	for i := 0; i+size < len(str); i++ {
		if str[i:i+size] == str2 {
			return i
		}
	}
	return -1
}

func Trim(str string) string {
	s := str
	for i := len(s); i > 0; i-- {
		if s[0] != ' ' {
			for ; i > 0; i-- {
				if s[i-1] != ' ' {
					break
				}
				s = s[:i-1]
			}
			break
		}
		s = s[1:]
	}
	return s
}

func ToString(x any) string {
	if x == nil {
		return ""
	}
	switch x := x.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int:
		return Ltoa(int64(x))
	case int8:
		return Ltoa(int64(x))
	case uint8:
		return Ltoa(int64(x))
	case int16:
		return Ltoa(int64(x))
	case uint16:
		return Ltoa(int64(x))
	case int32:
		return Ltoa(int64(x))
	case uint32:
		return Ltoa(int64(x))
	case uint64:
		return Ltoa(int64(x))
	case int64:
		return Ltoa(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case error:
		return x.Error()
	default:
		return "<?>"
	}
}

func ToLowerAscii(s string) string {
	bytes := []byte(s)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + 32
		}
	}
	return string(bytes)
}
