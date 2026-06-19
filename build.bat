@echo off
REM Tergum build script for Windows
REM Usage: build.bat [--prod] [linux|darwin|windows|all]

setlocal enabledelayedexpansion

REM Check for --prod flag
set PROD=0
set ARGS=
for %%a in (%*) do (
    if "%%a"=="--prod" (
        set PROD=1
    ) else (
        set ARGS=%%a
    )
)

REM Version info
if %PROD%==1 (
    for /f "tokens=*" %%i in ('git status --porcelain 2^>nul') do (
        echo WARNING: Working tree is dirty. Prod build will use clean version anyway.
        goto :version_done_warn
    )
    :version_done_warn
    for /f "tokens=*" %%i in ('git describe --tags --always 2^>nul') do set VERSION=%%i
) else (
    for /f "tokens=*" %%i in ('git describe --tags --always --dirty 2^>nul') do set VERSION=%%i
)
if "%VERSION%"=="" set VERSION=dev
for /f "tokens=*" %%i in ('git rev-parse --short HEAD 2^>nul') do set COMMIT=%%i
if "%COMMIT%"=="" set COMMIT=none

for /f "tokens=*" %%i in ('powershell -command "Get-Date -Format yyyy-MM-ddTHH:mm:ssZ"') do set BUILDDATE=%%i
if "%BUILDDATE%"=="" set BUILDDATE=unknown

set LDFLAGS=-s -w -X "github.com/ricardopadilha/tergum/cmd.Version=%VERSION%" -X "github.com/ricardopadilha/tergum/cmd.Commit=%COMMIT%" -X "github.com/ricardopadilha/tergum/cmd.BuildDate=%BUILDDATE%"

set OUTPUT_DIR=dist
if not exist %OUTPUT_DIR% mkdir %OUTPUT_DIR%

set TARGET=%ARGS%
if "%TARGET%"=="" set TARGET=all

if "%TARGET%"=="linux" goto :build_linux
if "%TARGET%"=="darwin" goto :build_darwin
if "%TARGET%"=="macos" goto :build_darwin
if "%TARGET%"=="windows" goto :build_windows
if "%TARGET%"=="win" goto :build_windows
if "%TARGET%"=="all" goto :build_all

echo Usage: build.bat [linux^|darwin^|windows^|all]
echo.
echo Options:
echo   linux    Build for Linux (amd64)
echo   darwin   Build for macOS (arm64)
echo   windows  Build for Windows (amd64)
echo   all      Build all platforms (default)
exit /b 1

:build_linux
echo Building for Linux (amd64)...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -o %OUTPUT_DIR%\tergum-linux ./
echo   -^> %OUTPUT_DIR%\tergum-linux
goto :done_step

:build_darwin
echo Building for macOS (arm64)...
set CGO_ENABLED=0
set GOOS=darwin
set GOARCH=arm64
go build -ldflags="%LDFLAGS%" -o %OUTPUT_DIR%\tergum-macos ./
echo   -^> %OUTPUT_DIR%\tergum-macos
goto :done_step

:build_windows
echo Building for Windows (amd64)...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -o %OUTPUT_DIR%\tergum.exe ./
echo   -^> %OUTPUT_DIR%\tergum.exe
goto :done_step

:build_all
call :build_linux
call :build_darwin
call :build_windows
goto :done_step

:done_step
echo.
echo Build complete! (version: %VERSION%, commit: %COMMIT%)
exit /b 0
