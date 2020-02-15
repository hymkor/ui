package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
	"github.com/mattn/go-tty"

	"github.com/zetamatta/go-readline-ny"
	"github.com/zetamatta/go-windows10-ansi"
)

const (
	_ANSI_YELLOW     = "\x1B[0;33;1m"
	_ANSI_RESET      = "\x1B[0m"
	_ANSI_UP         = "\r\x1B[%dA"
	_ANSI_CURSOR_OFF = "\x1B[?25l"
	_ANSI_CURSOR_ON  = "\x1B[?25h"
	_ANSI_ERASE_LINE = "\x1B[0K"
)

func cutStrInWidth(s string, cellwidth int) (string, int) {
	w := 0
	for n, c := range s {
		w1 := runewidth.RuneWidth(c)
		if w+w1 > cellwidth {
			return s[:n], w
		}
		w += w1
	}
	return s, w
}

type View struct {
	cache []string
}

func detab(s string) string {
	for {
		pos := strings.IndexByte(s, '\t')
		if pos < 0 {
			return s
		}
		s = s[:pos] + strings.Repeat(" ", 4-runewidth.StringWidth(s[:pos])%4) + s[pos+1:]
	}
}

func chomp(line string) (string, string) {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			return line[:len(line)-1], "\r\n"
		} else {
			return line, "\n"
		}
	} else if len(line) > 0 && line[len(line)-1] == '\r' {
		return line[:len(line)-1], "\r"
	}
	return line, ""

}

func (this *View) Draw(enum func() (string, error), width, height int, out io.Writer) int {
	for rowIndex := 0; rowIndex < height; rowIndex++ {
		line, err := enum()
		if err != nil {
			return rowIndex
		}
		if this.cache != nil && len(this.cache) < rowIndex {
			if this.cache[rowIndex] == line {
				io.WriteString(out, "\n")
				continue
			}
			this.cache[rowIndex] = line
		} else {
			this.cache = append(this.cache, line)
		}
		var term string
		line, term = chomp(line)
		switch term {
		case "\r\n":
			term = "\u2936" // Arrow pointing downwards then curving leftwards
		case "\n":
			term = "\u2B63" // Downwards Arrow
		case "\r":
			term = "\u2B60" // Leftwards Arrow
		}
		var trimedWidth int
		line = detab(line)
		line, trimedWidth = cutStrInWidth(line, width)
		io.WriteString(out, line)
		if trimedWidth < width {
			io.WriteString(out, _ANSI_YELLOW)
			io.WriteString(out, term)
		}
		io.WriteString(out, _ANSI_RESET)
		io.WriteString(out, _ANSI_ERASE_LINE)
		if true || trimedWidth < width-1 {
			io.WriteString(out, "\n")
		}
	}
	return height
}

func getline(out io.Writer, prompt string, defaultStr string, csrlin *int) (string, error) {
	text, term := chomp(defaultStr)
	editor := readline.Editor{
		Writer:  out,
		Default: text,
		Cursor:  65535,
		Prompt: func() (int, error) {
			io.WriteString(out, "\r\x1B[0;33;40;1m")
			io.WriteString(out, prompt)
			io.WriteString(out, _ANSI_RESET)
			io.WriteString(out, _ANSI_ERASE_LINE)
			return 2, nil
		},
		LineFeed: func(readline.Result) {
			io.WriteString(out, "\r")
		},
	}
	defer io.WriteString(out, _ANSI_CURSOR_OFF)

	editor.BindKeySymbol(readline.K_ESCAPE, readline.F_INTR)

	up := &readline.KeyGoFuncT{
		Func: func(_ context.Context, _ *readline.Buffer) readline.Result {
			*csrlin--
			return readline.ENTER
		},
		Name: "UP",
	}
	down := &readline.KeyGoFuncT{
		Func: func(_ context.Context, _ *readline.Buffer) readline.Result {
			*csrlin++
			return readline.ENTER
		},
		Name: "DOWN",
	}
	editor.BindKeyFunc(readline.K_UP, up)
	editor.BindKeyFunc(readline.K_DOWN, down)
	editor.BindKeyFunc(readline.K_CTRL_P, up)
	editor.BindKeyFunc(readline.K_CTRL_N, down)

	var err error
	text, err = editor.ReadLine(context.Background())
	return text + term, err
}

func main2(in io.Reader, out io.Writer) error {
	var view1 View

	tty1, err := tty.Open()
	if err != nil {
		return err
	}
	defer tty1.Close()

	width, height, err := tty1.Size()
	if err != nil {
		return err
	}

	br := bufio.NewReader(in)
	csrline := 0
	headline := 0
	buffer := []string{}
	for {
		count := headline
		nlines := view1.Draw(func() (string, error) {
			var err error = nil
			if count >= len(buffer) {
				var text string
				text, err = br.ReadString('\n')
				buffer = append(buffer, text)
			}
			count++
			return buffer[count-1], err
		}, width, height-2, out)
		fmt.Fprintln(out)
		if headline+nlines-csrline+1 > 0 {
			fmt.Fprintf(out, _ANSI_UP, headline+nlines-csrline+1)
		}
		move := 0
		text, err := getline(out, "", buffer[csrline], &move)
		if err != nil {
			return nil
		}
		buffer[csrline] = text
		if csrline-headline > 0 {
			fmt.Fprintf(out, _ANSI_UP, csrline-headline)
		}
		_csrline := csrline + move
		if _csrline < 0 {
			continue
		}
		if _csrline < headline {
			headline = _csrline
		} else if _csrline >= headline+height-2 {
			headline = _csrline - (height - 2) + 1
		}
		csrline = _csrline
	}
	return nil
}

func main1(args []string) error {
	disabler, err := ansi.EnableStdoutVirtualTerminalProcessing()
	if err != nil {
		return err
	}
	defer disabler()

	if len(args) >= 2 {
		fd, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer fd.Close()
		return main2(fd, os.Stdout)
	} else if !isatty.IsTerminal(os.Stdin.Fd()) {
		return main2(os.Stdin, os.Stdout)
	} else {
		return fmt.Errorf("Usage: %s FILENAME  or %s < FILENAME", args[0], args[0])
	}
}

func main() {
	if err := main1(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
