@echo off
echo Building local-mesh...
go build -o local-mesh.exe ./cmd/local-mesh
if %ERRORLEVEL% EQU 0 (
    echo.
    echo Build successful! Run with: local-mesh.exe
) else (
    echo.
    echo Build FAILED. Check errors above.
    exit /b 1
)
