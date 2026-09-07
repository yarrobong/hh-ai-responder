$ErrorActionPreference = "Stop"

$App = "hh-ai-responder.exe"

try {
    # Переходим в директорию, где находится скрипт
    $ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    Set-Location $ScriptDir
}
catch {
    Write-Error "ERROR: cannot change directory"
    exit 1
}

# The application loads .env itself, using the same parser on every platform.

# Проверяем наличие бинарника
if (-not (Test-Path $App)) {
    Write-Error "ERROR: $App not found"
    exit 2
}

# Проверяем, что это исполняемый файл
if (-not ($App.ToLower().EndsWith(".exe"))) {
    Write-Error "ERROR: $App is not an executable file"
    exit 3
}

# Запускаем приложение с передачей аргументов
& ".\\$App" @args
exit $LASTEXITCODE
