# Docker Commands

## Installation

To install Docker on your Linux system, run:
```bash
sudo apt-get update
sudo apt-get install docker-ce docker-ce-cli containerd.io
```

## Running Docker

Start Docker service:
```bash
sudo systemctl start docker
```

Run a Docker container:
```bash
docker run -it ubuntu /bin/bash
```

# Linux Commands

## File Operations

List files in a directory:
```bash
ls -l
```

Create a new file:
```bash
touch newfile.txt
```

Remove a file:
```bash
rm newfile.txt
```