# 安装 ffmpeg

# 下载 ffmpeg
Write-Host "正在下载 ffmpeg..."
$url = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
$zip = "$env:TEMP\ffmpeg.zip"

try {
    $ProgressPreference = 'SilentlyContinue'
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

    Write-Host "下载完成,开始解压..."
    $tempExtract = "$env:TEMP\ffmpeg-temp"
    Expand-Archive -Path $zip -DestinationPath $tempExtract -Force

    # 找到 ffmpeg.exe
    $ffmpeg = Get-ChildItem -Path $tempExtract -Filter "ffmpeg.exe" -Recurse | Select-Object -First 1

    if ($ffmpeg) {
        # 复制到项目目录
        Copy-Item $ffmpeg.FullName -Destination ".\ffmpeg.exe" -Force
        Write-Host "ffmpeg.exe 已安装到项目目录"

        # 验证
        .\ffmpeg.exe -version | Select-Object -First 3

        # 清理
        Remove-Item $zip -Force -ErrorAction SilentlyContinue
        Remove-Item $tempExtract -Recurse -Force -ErrorAction SilentlyContinue

        Write-Host "`n安装成功!"
    } else {
        Write-Host "错误: 未找到 ffmpeg.exe"
    }
} catch {
    Write-Host "错误: $_"
    Write-Host "`n请手动下载:"
    Write-Host "1. 访问 https://www.gyan.dev/ffmpeg/builds/"
    Write-Host "2. 下载 ffmpeg-release-essentials.zip"
    Write-Host "3. 解压并复制 bin\ffmpeg.exe 到项目根目录"
}
