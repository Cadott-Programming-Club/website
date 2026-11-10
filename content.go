package main

import "website/pages"

const facebookURL = "https://www.facebook.com/profile.php?id=61580536562505"

var lessons = []pages.Lesson{
	{Number: 1, Slug: "01-what-is-a-model", When: "Tue Nov 10", Title: "What Is a Model?"},
	{Number: 2, Slug: "02-prompting-and-context", When: "Tue Nov 17", Title: "Talk to Your AI Like a Game Designer"},
	{Number: 3, Slug: "03-how-models-learn", When: "Tue Dec 1", Title: "How a Model Learns"},
	{Number: 4, Slug: "04-enemy-brains", When: "Tue Dec 8", Title: "Enemy Brains"},
	{Number: 5, Slug: "05-agents", When: "Winter", Title: "How Your AI Agent Actually Works"},
	{Number: 6, Slug: "06-write-your-own-skill", When: "Winter", Title: "Write Your Own Skill"},
	{Number: 7, Slug: "07-game-feel", When: "Winter", Title: "Game Feel"},
	{Number: 8, Slug: "08-ai-art-for-games", When: "Winter", Title: "AI Art for Your Game"},
	{Number: 9, Slug: "09-3d-with-blender", When: "Winter", Title: "3D Models with Blender and AI"},
	{Number: 10, Slug: "10-ai-that-learns-to-play", When: "Winter", Title: "AI That Learns to Play"},
	{Number: 11, Slug: "11-neural-networks", When: "Winter", Title: "Inside the Brain: Neural Networks"},
	{Number: 12, Slug: "12-procedural-worlds-and-npcs", When: "Winter", Title: "Endless Worlds and Talking NPCs"},
	{Number: 13, Slug: "13-ship-it", When: "Winter", Title: "Ship It"},
}

var games = []pages.Game{
	{Slug: "brick-breaker-plus", Title: "Brick Breaker Plus", Description: "Eight levels, a boss, power-ups and mouse control. Built by AI with OpenCode, then improved with three follow-up requests."},
}
