package main

import "github.com/fatih/color"

func paintTimestamp(timestamp string) string {
	return color.GreenString(rightPad2Len(timestamp, " ", 23))
}

func paintRequestId(requestId string) string {
	return color.MagentaString(requestId)
}

func paintSource(source string) string {
	return color.CyanString(source)
}

func paintInfoline(content string) string {
	return color.YellowString(content)
}

func paintSystemParams(config *Configuration) string {
	return color.MagentaString("Profile: " + config.Profile + " Host: " + config.SearchTarget.Url)
}

func highlightContent(content string) string {
	yellow := color.New(color.FgBlue, color.BgCyan).SprintFunc()
	return yellow(content)
}
