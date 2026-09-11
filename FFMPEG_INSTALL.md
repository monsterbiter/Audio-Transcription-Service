# FFmpeg 安装指南

## 方法1: 自动下载 (推荐)

运行以下 PowerShell 命令自动下载并配置 ffmpeg:

```powershell
# 下载 ffmpeg
$url = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"
$zip = "$env:TEMP\ffmpeg.zip"
Invoke-WebRequest -Uri $url -OutFile $zip

# 解压
Expand-Archive -Path $zip -DestinationPath ".\ffmpeg-temp" -Force

# 复制 ffmpeg.exe 到项目目录
Copy-Item ".\ffmpeg-temp\ffmpeg-*\bin\ffmpeg.exe" ".\ffmpeg.exe"

# 清理
Remove-Item $zip
Remove-Item ".\ffmpeg-temp" -Recurse -Force

Write-Host "ffmpeg 安装完成!"
```

## 方法2: 手动下载

1. 访问: https://github.com/BtbN/FFmpeg-Builds/releases
2. 下载: `ffmpeg-master-latest-win64-gpl.zip`
3. 解压后,将 `bin\ffmpeg.exe` 复制到项目根目录

## 方法3: 使用包管理器

```powershell
# 使用 Chocolatey
choco install ffmpeg

# 或使用 winget
winget install ffmpeg
```

## 验证安装

```powershell
.\ffmpeg.exe -version
```

## 说明

服务会自动使用 ffmpeg 将上传的音频转换为科大讯飞要求的格式:
- 采样率: 16000 Hz
- 声道: 单声道 (mono)
- 位深: 16 bit
- 格式: PCM WAV

如果 ffmpeg 不可用,服务会尝试直接使用原始音频文件(可能导致转写失败)。
