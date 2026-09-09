Output assets/burrowtime-agent.gif

Set Shell "bash"
Set FontSize 17
Set Width 1100
Set Height 700
Set Padding 22
Set TypingSpeed 22ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-agent"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-agent /tmp/burrowtime-vhs-watson && mkdir -p /tmp/burrowtime-vhs-agent /tmp/burrowtime-vhs-watson"
Enter
Ctrl+L
Show

Type "./bin/burrowtime agent start --client codex --project 'client portal' --task PORTAL-42 --lease 30m"
Enter
Sleep 2s

Hide
Type "session=$(./bin/burrowtime agent status --json | jq -r '.[-1].id')"
Enter
Show
Type "./bin/burrowtime agent status --active"
Enter
Sleep 2s

Type "./bin/burrowtime agent pause --session $session"
Enter
Sleep 2s

Type "./bin/burrowtime agent resume --session $session"
Enter
Sleep 2s

Type "./bin/burrowtime agent stop --session $session"
Enter
Sleep 2s

Type "./bin/burrowtime agent report --project 'client portal'"
Enter
Sleep 4s
