/*
 * Cadence - The resource-oriented smart contract programming language
 *
 * Copyright Dapper Labs, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/openconfig/goyang/pkg/indent"
	"github.com/turbolent/prettier"
	"golang.org/x/exp/slices"

	"github.com/onflow/cadence/runtime/parser"
	"github.com/onflow/cadence/runtime/parser/lexer"
)

func pretty(code string, maxLineWidth int) string {
	program, err := parser.ParseProgram(nil, []byte(code), parser.Config{})
	if err != nil {
		return err.Error()
	}

	var b strings.Builder
	prettier.Prettier(&b, program.Doc(), maxLineWidth, "    ")
	return b.String()
}

// language=html
const page = `
<html>
<head>
    <title>Pretty</title>
    <style>
        :root {
            --line-length: 0ch;
        }

        body {
            margin: 0;
            padding: 0;
            font-family: monospace;
            height: 100vh;
        }

        #panels {
            display: grid;
            grid-template-rows: 100vh;
            grid-template-columns: 50% 50%;
            grid-template-areas: "editor ast";
        }

        #editor {
            grid-area: editor;
            border: 1px solid #ccc;
            resize: none;
        }

        #pretty {
            position: relative;
            grid-area: ast;
        }

        #output {
            white-space: pre;
            height: 100%;
            overflow: scroll;
        }

        #bar {
            position: absolute;
            left: var(--line-length);
            top: 0;
            bottom: 0;
            width: 2px;
            background-color: black;
        }

        #stepper {
            position: sticky;
            top: 0
        }
    </style>
</head>
<body id="panels">
<textarea id="editor" onkeydown="if(event.keyCode===9){var v=this.value,s=this.selectionStart,e=this.selectionEnd;this.value=v.substring(0, s)+'    '+v.substring(e);this.selectionStart=this.selectionEnd=s+4;return false;}"></textarea>
<div id="pretty">
    <input id="stepper" type="number" min="1" step="1">
    <div id="output"></div>
    <div id="bar"></div>
</div>
</body>
<script>
    let code = localStorage.getItem('code') || ''
    let maxLineLength = Number(localStorage.getItem('maxLineLength')) || 80;

    const root = document.documentElement;
    const editor = document.getElementById("editor")
    const output = document.getElementById("output")
    const stepper = document.getElementById("stepper")

    document.addEventListener('DOMContentLoaded', () => {
        stepper.value = maxLineLength
        editor.innerHTML = code
        update()
    })

    editor.addEventListener("input", (e) => {
        code = e.target.value
        localStorage.setItem('code', code)
        update()
    })

    stepper.addEventListener("input", (e) => {
        maxLineLength = Number(e.target.value)
        localStorage.setItem('maxLineLength', maxLineLength)
        update()
    })

    async function update() {
        root.style.setProperty('--line-length', maxLineLength + 'ch')
        const response = await fetch('/pretty', {
            method: "POST",
            body: JSON.stringify({
                code,
                maxLineLength
            })
		})
		output.innerText = await response.text()
    }
</script>
</html>
`

type Request struct {
	Code          string `json:"code"`
	MaxLineLength int    `json:"maxLineLength"`
}

func extractTokenText(text string, token lexer.Token, extra int) string {
	return text[token.StartPos.Offset : token.EndPos.Offset+1+extra]
}

func main() {

	portFlag := flag.Int("port", 9090, "port")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page))
	})

	http.HandleFunc("/pretty", func(w http.ResponseWriter, r *http.Request) {
		var req Request

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		existingCode := req.Code
		existingCodeLines := strings.Split(existingCode, "\n")
		oldTokens := lexer.Lex([]byte(existingCode), nil)

		prettyCode := pretty(existingCode, req.MaxLineLength)
		newTokens := lexer.Lex([]byte(prettyCode), nil)

		oldToken := lexer.Token{Type: lexer.TokenSpace}
		newToken := lexer.Token{Type: lexer.TokenSpace}

		ignoredTokenTypes := []lexer.TokenType{
			lexer.TokenParenClose,
			lexer.TokenParenOpen,
			lexer.TokenBraceOpen,
			lexer.TokenBraceClose,
			lexer.TokenBracketOpen,
			lexer.TokenBracketClose,
		}

		ignoredTokenTypes2 := []lexer.TokenType{
			lexer.TokenParenClose,
			lexer.TokenParenOpen,
		}

		fmt.Println(ignoredTokenTypes2)

		result := strings.Builder{}
		spaces := strings.Builder{}
		comment := strings.Builder{}

		for {

			if !newToken.Is(lexer.TokenEOF) {
				newToken = newTokens.Next()
			}

			if newToken.Is(lexer.TokenSpace) {
				spaces.WriteString(extractTokenText(prettyCode, newToken, 0))
				continue
			}

			if slices.Contains(ignoredTokenTypes, newToken.Type) {
				result.WriteString(spaces.String())
				result.WriteString(extractTokenText(prettyCode, newToken, 0))
				spaces.Reset()
				continue
			}

			if !oldToken.Is(lexer.TokenEOF) {
				for {
					oldToken = oldTokens.Next()

					//check only comments
					if oldToken.Is(lexer.TokenLineComment) || oldToken.Is(lexer.TokenBlockCommentContent) {

						switch oldToken.Type {
						case lexer.TokenLineComment:
							comment.WriteString(extractTokenText(existingCode, oldToken, 1))

						case lexer.TokenBlockCommentContent:
							commentString := extractTokenText(existingCode, oldToken, 0)
							comment.WriteString("/*")
							comment.WriteString(commentString)
							comment.WriteString("*/")

							if oldToken.StartPos.Line < oldToken.EndPos.Line {
								//multiline block comment
								comment.WriteString("\n\n")
							}
						}

						oldLine := existingCodeLines[oldToken.StartPosition().Line-1][:oldToken.StartPosition().Column]
						oldLine = strings.Trim(oldLine, " ")
						//trailing comment
						if len(oldLine) > 0 {
							result.WriteString(" ")
							result.WriteString(comment.String())
							comment.Reset()
						}
					}

					if oldToken.Type == newToken.Type || oldToken.Is(lexer.TokenEOF) {
						break
					}
				}
			}

			if oldToken.Is(lexer.TokenEOF) && newToken.Is(lexer.TokenEOF) {
				//add remaining comments and finish
				result.WriteString(comment.String())
				break
			}

			//add spaces without existing indent in case we put comment
			spacesString := spaces.String()
			existingIndent := len(spacesString) - (strings.LastIndex(spacesString, "\n") + 1)
			result.WriteString(strings.TrimRight(spacesString, " "))
			spaces.Reset()

			if comment.Len() > 0 {
				//add existing comment (leading), pad to next element
				padding := ""
				//if existingIndent != 0 {
				padding = strings.Repeat(" ", newToken.StartPosition().Column)
				//}
				result.WriteString(indent.String(padding, comment.String()))
				result.WriteString(padding)
				comment.Reset()
			} else {
				result.WriteString(strings.Repeat(" ", existingIndent))
			}

			//add prettified code
			result.WriteString(extractTokenText(prettyCode, newToken, 0))

		}

		_, _ = w.Write([]byte(result.String()))
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		panic(err)
	}
	log.Printf("Listening on http://%s/", ln.Addr().String())
	var srv http.Server
	_ = srv.Serve(ln)
}
