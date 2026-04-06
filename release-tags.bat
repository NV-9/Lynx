@echo off
setlocal enabledelayedexpansion

if "%~1"=="" goto :usage

set "VERSION_TAG=%~1"
set "LATEST_TAG=latest"

if not "%~2"=="" set "LATEST_TAG=%~2"

if /I "%VERSION_TAG:~0,1%" NEQ "v" (
  echo Error: version tag must start with v. Example: v1.1.0
  exit /b 1
)

for /f "tokens=*" %%i in ('git rev-parse --abbrev-ref HEAD 2^>nul') do set "BRANCH=%%i"
if "%BRANCH%"=="" (
  echo Error: not inside a git repository.
  exit /b 1
)

for /f "tokens=*" %%i in ('git status --porcelain') do set "DIRTY=1"
if defined DIRTY (
  echo Error: working tree is not clean. Commit or stash changes first.
  exit /b 1
)

echo Creating annotated version tag %VERSION_TAG%...
git tag -a "%VERSION_TAG%" -m "Release %VERSION_TAG%"
if errorlevel 1 (
  echo Error: failed to create version tag.
  exit /b 1
)

echo Updating moving tag %LATEST_TAG% to current commit...
git tag -f "%LATEST_TAG%"
if errorlevel 1 (
  echo Error: failed to create or update %LATEST_TAG% tag.
  exit /b 1
)

echo Pushing %VERSION_TAG%...
git push origin "%VERSION_TAG%"
if errorlevel 1 (
  echo Error: failed to push %VERSION_TAG%.
  exit /b 1
)

echo Pushing %LATEST_TAG% with force...
git push origin -f "%LATEST_TAG%"
if errorlevel 1 (
  echo Error: failed to push %LATEST_TAG%.
  exit /b 1
)

echo.
echo Done.
echo Pushed tags:
echo   %VERSION_TAG%
echo   %LATEST_TAG%
exit /b 0

:usage
echo Usage:
echo   %~nx0 vMAJOR.MINOR.PATCH [latest-tag-name]
echo.
echo Examples:
echo   %~nx0 v1.1.0
echo   %~nx0 v1.1.0 latest
exit /b 1
