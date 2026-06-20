# TUI Overlay: Composite Line-by-Line to Preserve Background

**Date**: 2026-06-19
**Change**: tui-project-state-panel-fase4
**Category**: pattern

## What happened

A primeira implementação do painel de estado do projeto (`Ctrl+P`) usou `lipgloss.Place` para centrar o painel sobre a vista de chat. O fundo do painel era opaco e escondia o chat por baixo, criando um efeito de "caixa preta" e quebrando o contexto visual. Foi necessário reescrever `overlayPanel` para compor linha a linha: copiar o fundo, substituir apenas as linhas e colunas ocupadas pelo painel, e manter o chat visível acima, abaixo e nas laterais.

## How to avoid

Para overlays modal em TUI Bubble Tea/lipgloss, evite `Place` sobre a vista completa. Construa a sobreposição linha a linha, respeitando a largura real do terminal (`lipgloss.Width` do painel) e preenchendo os espaços laterais com espaços para não deixar rasto do fundo antigo dentro da área do painel.

## Tags

#lesson #change-tui-project-state-panel-fase4 #pattern #tui #overlay #ansi
