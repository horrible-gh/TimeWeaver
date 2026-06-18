#!/bin/bash

set -e  # 에러 발생 시 즉시 종료

echo "=== TimeWeaver Agent Service 설치 ==="
echo

# 현재 사용자 확인
CURRENT_USER=${SUDO_USER:-$USER}
echo "현재 사용자: $CURRENT_USER"

# 기본값 설정
DEFAULT_INSTALL_DIR="$HOME/work/python/TimeWeaver/agent"
DEFAULT_MOUNT_PATH="/mnt2"

# 사용자 입력 받기
read -p "설치 디렉토리 [$DEFAULT_INSTALL_DIR]: " INSTALL_DIR
INSTALL_DIR=${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}

read -p "마운트 경로 (없으면 Enter) [$DEFAULT_MOUNT_PATH]: " MOUNT_PATH
MOUNT_PATH=${MOUNT_PATH:-$DEFAULT_MOUNT_PATH}

read -p "서비스 실행 사용자 [$CURRENT_USER]: " SERVICE_USER
SERVICE_USER=${SERVICE_USER:-$CURRENT_USER}

# 경로 검증
if [ ! -d "$INSTALL_DIR" ]; then
    echo "❌ 오류: 설치 디렉토리가 존재하지 않습니다: $INSTALL_DIR"
    exit 1
fi

if [ ! -f "$INSTALL_DIR/timeweaver.py" ]; then
    echo "❌ 오류: timeweaver.py 파일을 찾을 수 없습니다"
    exit 1
fi

# Python 실행 파일 결정 (venv 우선, 없으면 시스템 python3)
if [ -f "$INSTALL_DIR/venv/bin/python3" ]; then
    PYTHON_EXEC="$INSTALL_DIR/venv/bin/python3"
    echo "✅ 가상환경 발견: $PYTHON_EXEC"
elif command -v python3 &> /dev/null; then
    PYTHON_EXEC=$(which python3)
    echo "⚠️  가상환경을 찾을 수 없어 시스템 Python을 사용합니다: $PYTHON_EXEC"
    echo
    read -p "가상환경을 지금 생성하시겠습니까? (Y/n): " create_venv
    if [[ ! "$create_venv" =~ ^[Nn]$ ]]; then
        echo "가상환경 생성 중..."
        cd "$INSTALL_DIR"
        python3 -m venv venv
        source venv/bin/activate
        if [ -f "requirements.txt" ]; then
            echo "의존성 설치 중..."
            pip install -r requirements.txt
        fi
        deactivate
        PYTHON_EXEC="$INSTALL_DIR/venv/bin/python3"
        echo "✅ 가상환경이 생성되었습니다"
    fi
else
    echo "❌ 오류: Python3를 찾을 수 없습니다"
    echo "   먼저 Python3를 설치하세요: sudo apt install python3 python3-venv"
    exit 1
fi

# 마운트 경로 설정 확인
USE_MOUNT="no"
MOUNT_CONFIG=""
if [ -d "$MOUNT_PATH" ]; then
    read -p "$MOUNT_PATH 마운트 의존성을 추가하시겠습니까? (y/N): " answer
    if [[ "$answer" =~ ^[Yy]$ ]]; then
        USE_MOUNT="yes"
        MOUNT_NAME=$(echo "$MOUNT_PATH" | sed 's/\//-/g' | sed 's/^-//')
        MOUNT_CONFIG="After=network.target local-fs.target ${MOUNT_NAME}.mount
RequiresMountsFor=$MOUNT_PATH"
    fi
fi

if [ "$USE_MOUNT" = "no" ]; then
    MOUNT_CONFIG="After=network.target local-fs.target"
fi

# ReadWritePaths 설정
READWRITE_CONFIG=""
if [ "$USE_MOUNT" = "yes" ]; then
    READWRITE_CONFIG="ReadWritePaths=$MOUNT_PATH"
fi

# 서비스 파일 생성
SERVICE_FILE="/tmp/timeweaver-agent.service"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=TimeWeaver Agent Service
$MOUNT_CONFIG

[Service]
Type=simple
User=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$PYTHON_EXEC timeweaver.py
Restart=always
RestartSec=10
Environment=PYTHONUNBUFFERED=1
ProtectSystem=no
$READWRITE_CONFIG
MountFlags=shared

[Install]
WantedBy=multi-user.target
EOF

echo
echo "=== 생성된 서비스 파일 미리보기 ==="
cat "$SERVICE_FILE"
echo
echo "========================================"
echo

# 설치 확인
read -p "위 내용으로 서비스를 설치하시겠습니까? (y/N): " confirm
if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
    echo "설치가 취소되었습니다."
    rm "$SERVICE_FILE"
    exit 0
fi

# systemd에 설치
echo
echo "서비스 설치 중..."
sudo cp "$SERVICE_FILE" /etc/systemd/system/timeweaver-agent.service
sudo systemctl daemon-reload

echo "✅ 서비스가 설치되었습니다!"
echo
echo "사용 가능한 명령어:"
echo "  sudo systemctl enable timeweaver-agent   # 부팅 시 자동 시작"
echo "  sudo systemctl start timeweaver-agent    # 서비스 시작"
echo "  sudo systemctl status timeweaver-agent   # 상태 확인"
echo "  journalctl -u timeweaver-agent -f        # 로그 보기"
echo

read -p "지금 서비스를 시작하시겠습니까? (y/N): " start_now
if [[ "$start_now" =~ ^[Yy]$ ]]; then
    sudo systemctl start timeweaver-agent
    echo
    sudo systemctl status timeweaver-agent
fi

read -p "부팅 시 자동 시작을 활성화하시겠습니까? (y/N): " enable_now
if [[ "$enable_now" =~ ^[Yy]$ ]]; then
    sudo systemctl enable timeweaver-agent
    echo "✅ 부팅 시 자동 시작이 활성화되었습니다"
fi

# 임시 파일 삭제
rm "$SERVICE_FILE"

echo
echo "🎉 설치가 완료되었습니다!"
