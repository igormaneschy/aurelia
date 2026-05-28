package pipeline

import "strings"

type concurrentMessageKind int

const (
	concurrentEnqueue concurrentMessageKind = iota
	concurrentCancel
	concurrentSupersede
	concurrentStatus
)

func classifyConcurrentMessage(text string) concurrentMessageKind {
	n := normalizeConcurrentText(text)
	if n == "" {
		return concurrentStatus
	}
	if isStatusMessage(n) {
		return concurrentStatus
	}
	if isCancelOnlyMessage(n) {
		return concurrentCancel
	}
	if isSupersedeMessage(n) {
		return concurrentSupersede
	}
	return concurrentEnqueue
}

func normalizeConcurrentText(text string) string {
	n := strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
	n = replacer.Replace(n)
	n = strings.Trim(n, ".,!?:; ")
	return strings.Join(strings.Fields(n), " ")
}

func isCancelOnlyMessage(n string) bool {
	exact := map[string]bool{
		"para": true, "pare": true, "parar": true, "stop": true,
		"cancela": true, "cancelar": true, "cancele": true,
		"interrompe": true, "interrompa": true,
		"esquece": true, "deixa pra la": true, "nao precisa": true,
	}
	if exact[n] {
		return true
	}
	needles := []string{"pode parar", "pode cancelar", "nao precisa mais", "para isso", "cancela isso", "cancele isso"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isSupersedeMessage(n string) bool {
	needles := []string{
		"na verdade", "corrigindo", "em vez", "ao inves", "melhor", "mudei", "troque",
		"nao corrija", "apenas", "so faca", "so teste", "topico errado", "lugar errado",
		"nao era", "errado", "pare e", "cancele e", "ignore o anterior",
	}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isStatusMessage(n string) bool {
	needles := []string{"conseguiu", "terminou", "acabou", "status", "andamento", "ja foi", "ta pronto", "esta pronto"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}
