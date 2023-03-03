package uniqueid

//使用linux程式
import (
	"os/exec"
)

func uuid() []byte {
	out, err := exec.Command("uuidgen").Output()
	if err != nil {
		panic(err)
	}
	return out
}
