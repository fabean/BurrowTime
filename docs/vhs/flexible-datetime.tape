Output assets/burrowtime-flexible-datetime.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 22
Set TypingSpeed 28ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-flexible-datetime/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-flexible-datetime/watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-flexible-datetime && mkdir -p /tmp/burrowtime-vhs-flexible-datetime/burrowtime /tmp/burrowtime-vhs-flexible-datetime/watson"
Enter
Ctrl+L
Show

Type "./bin/burrowtime config options.time_precision minute"
Enter
Sleep 2s

Type "./bin/burrowtime add --from '9am' --to '10:30 AM' Demo +DEMO-101"
Enter
Sleep 2s

Type "./bin/burrowtime add --from '14:30' --to '15:15' Sample +SAMPLE-2"
Enter
Sleep 2s

Type "./bin/burrowtime log --day"
Enter
Sleep 4s

Hide
Type "rm -rf /tmp/burrowtime-vhs-flexible-datetime"
Enter
Sleep 1s
