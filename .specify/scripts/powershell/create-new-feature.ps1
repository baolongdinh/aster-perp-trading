#!/usr/bin/env pwsh
# Create new feature branch and spec file

param(
    [Parameter(Mandatory=$false)]
    [string]$Description,
    
    [Parameter(Mandatory=$false)]
    [string]$ShortName,
    
    [Parameter(Mandatory=$false)]
    [switch]$Timestamp,
    
    [Parameter(Mandatory=$false)]
    [switch]$Json
)

# Generate branch name
$prefix = if ($Timestamp) { 
    (Get-Date -Format "yyyyMMdd-HHmmss") 
} else { 
    $existing = git branch -a | Select-String "feature/(\d+)-" | ForEach-Object { 
        if ($_ -match "feature/(\d+)-") { [int]$matches[1] } 
    } | Sort-Object -Descending | Select-Object -First 1
    $next = if ($existing) { $existing + 1 } else { 1 }
    $next.ToString("D3")
}

$branchName = "feature/$prefix-$ShortName"
$featureDir = ".specify/features/$branchName"

# Create feature directory structure
New-Item -ItemType Directory -Force -Path "$featureDir/checklists" | Out-Null
New-Item -ItemType Directory -Force -Path "$featureDir/tech-specs" | Out-Null
New-Item -ItemType Directory -Force -Path "$featureDir/adr" | Out-Null

# Create spec.md from template
$specContent = Get-Content ".specify/templates/spec-template.md" -Raw
$specContent = $specContent -replace "\[FEATURE NAME\]", $Description
$specPath = "$featureDir/spec.md"
$specContent | Out-File -FilePath $specPath -Encoding UTF8

# Create empty implementation.md
"# Implementation: $Description

## Design Decisions

## Implementation Plan

## Progress
" | Out-File -FilePath "$featureDir/implementation.md" -Encoding UTF8

# Output result
if ($Json) {
    @{
        BRANCH_NAME = $branchName
        SPEC_FILE = $specPath
        FEATURE_DIR = $featureDir
    } | ConvertTo-Json -Compress
} else {
    Write-Host "Created feature branch: $branchName"
    Write-Host "Spec file: $specPath"
}
