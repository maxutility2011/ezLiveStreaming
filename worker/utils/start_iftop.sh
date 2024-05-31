# -s 1: iftop reads bandwidth then exit in 1 second
# -i lo: iftop reads bandwidth of interface "lo"
# -f "port $1": iftop reads bandwidth of port $1 (command line argument)
sudo iftop -t -s 1 -f "port $1" 2>/dev/null | awk '/send and receive/ {print $6}' 