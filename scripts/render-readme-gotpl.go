package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
)

type readmeData struct {
	ChecksumsURL          string
	ContainerBadgeURL     string
	ImageCIRef            string
	ImageRef              string
	LatestReleaseBadgeURL string
	ReleaseTag            string
	ReleaseURL            string
	Repo                  string
	RepoOwner             string
	TapPackageRepo        string
	TapRepo               string
	Version               string
}

func main() {
	templatePath := flag.String("template", "", "path to the README gotpl template")
	outputPath := flag.String("output", "", "path to write the rendered README to")
	repoSlug := flag.String("repo", "hydra-gitops/hydra", "repository slug in owner/name format")
	tapRepoSlug := flag.String("tap-repo", "hydra-gitops/homebrew-tap", "homebrew tap repository slug in owner/name format")
	version := flag.String("version", "", "release version with or without leading v")
	flag.Parse()

	if *templatePath == "" || *outputPath == "" {
		fmt.Fprintln(os.Stderr, "template and output are required")
		os.Exit(1)
	}

	normalizedVersion := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	releaseTag := "latest"
	releaseURL := fmt.Sprintf("https://github.com/%s/releases/latest", *repoSlug)
	checksumsURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/checksums.txt", *repoSlug)

	if normalizedVersion != "" {
		releaseTag = "v" + normalizedVersion
		releaseURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", *repoSlug, releaseTag)
		checksumsURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", *repoSlug, releaseTag)
	}

	data := readmeData{
		ChecksumsURL:          checksumsURL,
		ContainerBadgeURL:     "https://img.shields.io/badge/container-ghcr.io-blue",
		ImageCIRef:            "ghcr.io/" + strings.ToLower(*repoSlug) + "-ci",
		ImageRef:              "ghcr.io/" + strings.ToLower(*repoSlug),
		LatestReleaseBadgeURL: fmt.Sprintf("https://img.shields.io/github/v/release/%s?sort=semver", *repoSlug),
		ReleaseTag:            releaseTag,
		ReleaseURL:            releaseURL,
		Repo:                  *repoSlug,
		RepoOwner:             repoOwner(*repoSlug),
		TapPackageRepo:        tapPackageRepo(*tapRepoSlug),
		TapRepo:               *tapRepoSlug,
		Version:               normalizedVersion,
	}

	tpl, err := template.ParseFiles(*templatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse template: %v\n", err)
		os.Exit(1)
	}

	out, err := os.Create(*outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create output: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	if err := tpl.Execute(out, data); err != nil {
		fmt.Fprintf(os.Stderr, "render template: %v\n", err)
		os.Exit(1)
	}
}

func tapPackageRepo(tapRepoSlug string) string {
	owner, _, ok := strings.Cut(tapRepoSlug, "/")
	if !ok || strings.TrimSpace(owner) == "" {
		return tapRepoSlug
	}
	return owner + "/tap"
}

func repoOwner(repoSlug string) string {
	owner, _, ok := strings.Cut(strings.ToLower(repoSlug), "/")
	if !ok || strings.TrimSpace(owner) == "" {
		return strings.ToLower(repoSlug)
	}
	return owner
}
