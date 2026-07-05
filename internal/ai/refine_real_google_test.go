package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRefineRealGoogle(t *testing.T) {
	loadEnvFile(t)

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" || apiKey == "your-api-key-here" {
		t.Skip("GOOGLE_API_KEY not set in .env — skipping real Google refine test")
	}

	provider := &GoogleProvider{
		APIKey:  apiKey,
		Model:   "gemma-4-31b-it",
		Timeout: 600 * time.Second,
	}

	original := `The old man sat on the bench, watching the world go by. His hands, rough and calloused from decades of labor, trembled slightly as he reached for the pipe in his coat pocket. The autumn air carried the scent of fallen leaves and distant woodsmoke.

"Beautiful day, isn't it?" said the woman sitting beside him. She was younger, perhaps forty, with sharp eyes that missed nothing.

He nodded but said nothing. There was a time when he would have talked to strangers for hours, sharing stories of the sea and the ships he'd sailed on. But that was before the accident, before Martha died, before the world became a gray and silent place.

The children in the park were playing some kind of game — chasing each other around the old oak tree, their laughter cutting through the afternoon quiet like bells. He watched them with a mixture of fondness and sadness. His own grandchildren never visited anymore. They were too busy, or too far away, or perhaps they simply didn't care.

"Are you alright?" the woman asked, leaning forward slightly.

"Fine," he said, the word coming out harder than he intended. He softened his expression. "Sorry. I didn't mean to be rude. I was just... thinking."

"About what?"

He paused. How could he explain that he was thinking about a life that no longer existed? About a woman who had been dead for three years? About the letters he'd never sent and the words he'd never said?

"Nothing important," he said at last.

The woman seemed to understand that he didn't want to talk. She stood up, brushed the crumbs from her skirt, and walked away without another word. He watched her go, admiring her directness. There was something refreshing about someone who knew when to leave.

The sun was beginning to set, painting the sky in shades of orange and purple. He pulled his coat tighter around his shoulders and stood up slowly, his joints protesting every movement. He would go home now, to the empty apartment, to the silence that greeted him at the door. Tomorrow he would come back to this bench, and the day after that, and the day after that. It was the only routine he had left.`

	translation := `El viejo hombre se sento en el banco, mirando el mundo pasar. Sus manos, rugosas y callosas por decadas de trabajo, temblaban ligeramente cuando alcanzo la pipa en el bolsillo de su abrigo. El aire de otoño traia el aroma de hojas caidas y lejana humadera.

—Que dia tan hermoso, verdad? —dijo la mujer sentada a su lado. Era mas joven, tal vez cuarenta, con ojos agudos que no perdian nada.

El asintio pero no dijo nada. Habia un tiempo en el que hablaria con desconocidos por horas, compartiendo historias del mar y los barcos en los que habia navegado. Pero eso fue antes del accidente, antes de que Martha muriera, antes de que el mundo se convirtiera en un lugar gris y silencioso.

Los ninos en el parque jugaban algun tipo de juego —corrian unos tras otros alrededor del viejo roble, sus risas cortando el silencio de la tarde como campanas. El los observaba con una mezcla de carino y tristeza. Sus propios nietos nunca las visitaban. Estaban demasiado ocupados, o demasiado lejos, o quizas simplemente no les importaba.

—Esta bien? —pregunto la mujer, inclinándose ligeramente hacia adelante.

—Bien —dijo el, la palabra saliendo mas dura de lo que pretendia. Suavizo su expresion. —Perdon. No quise ser grosero. Solo estaba... pensando.

—En que?

El pauso. Como podia explicar que estaba pensando en una vida que ya no existia? En una mujer que habia muerto por tres años? En las cartas que habia enviado y las palabras que nunca dijo?

—Nada importante —dijo al fin.

La mujer pareció entender que el no queria hablar. Se levanto, se arrugo las migajas de su falda, y se fue sin otra palabra. El la vio ir, admirando su franqueza. Habia algo refrescante en alguien que sabia cuando irse.

El sol estaba comenzando a ponerse, pintando el cielo en tonos de naranja y morado. El se ajusto el abrigo mas fuerte alrededor de sus hombros y se levanto lentamente, sus articulaciones protestando cada movimiento. El iria a casa ahora, al apartamento vacio, al silencio que lo recibia en la puerta. Manana volveria a este banco, y el dia despues de ese, y el dia despues de ese. Era la unica rutina que le quedaba.`

	applied := 0
	proposed := 0
	var unresolved []RefineEdit

	apply := func(edits []RefineEdit) []RefineEditResult {
		proposed += len(edits)
		results := make([]RefineEditResult, 0, len(edits))
		for _, edit := range edits {
			if edit.Original == "" {
				results = append(results, RefineEditResult{Edit: edit, Reason: "empty_original"})
				continue
			}
			if edit.Original == edit.Replacement {
				results = append(results, RefineEditResult{Edit: edit, Reason: "no_op"})
				continue
			}
			count := strings.Count(translation, edit.Original)
			if count == 0 {
				unresolved = append(unresolved, edit)
				results = append(results, RefineEditResult{Edit: edit, Reason: "not_found"})
				continue
			}
			if count > 1 {
				unresolved = append(unresolved, edit)
				results = append(results, RefineEditResult{Edit: edit, Reason: "multiple_matches"})
				continue
			}
			translation = strings.Replace(translation, edit.Original, edit.Replacement, 1)
			applied++
			results = append(results, RefineEditResult{Edit: edit, Applied: true})
		}
		return results
	}

	systemPrompt := "You are an expert literary translation editor. You refine a preliminary {TARGET_LANG} translation of a {SOURCE_LANG} original.\n\nYou do not rewrite the whole chapter. You call the apply_edits tool with precise, surgical corrections.\n\nThe following are mandatory translations: [{SOURCE_LANG}] → [{TARGET_LANG}]\n(none)\n\nEditing rules:\n- Fix spelling, grammar, punctuation, and fluency.\n- Fix determiners and agreement errors.\n- Preserve the author's tone, voice, and style without paraphrasing or summarizing.\n- Do not alter narrative content.\n- Use masculine gender by default when context does not specify gender.\n- Do not use European Spanishisms.\n- Do not use: follar, joder, vosotros, -éis, -óis, pediros.\n- Preserve ALL double line breaks exactly as they appear in the original text; never remove or reduce them.\n- Adjust articles on proper nouns according to {TARGET_LANG} grammar.\n\nEach edit's \"original\" must be a complete sentence or complete line copied exactly, character for character, from the current translation. It must occur exactly once.\nIf you cannot find a complete sentence or line that matches exactly, do not propose that edit.\nCall apply_edits with all the edits you have ready. If some are reported as failed, resend corrected versions of only those — do not resend edits that already succeeded.\nWhen you have no more corrections to make, stop calling the tool."
	systemPrompt = strings.ReplaceAll(systemPrompt, "{SOURCE_LANG}", "en")
	systemPrompt = strings.ReplaceAll(systemPrompt, "{TARGET_LANG}", "es")

	userPrompt := `Original (en):
` + original + `

Current translation (es):
` + translation + `

Review the current translation against the original and call apply_edits with any corrections needed. If no corrections are needed, do not call the tool.`

	t.Logf("=== BEFORE refine (Google) ===")
	t.Logf("Translation:\n%s", translation)

	ctx, cancel := context.WithTimeout(context.Background(), 660*time.Second)
	defer cancel()

	summary, err := provider.Refine(ctx, RefineInput{
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
		SourceLanguage: "en",
		TargetLanguage: "es",
		ApplyEdits:     apply,
		CurrentText:    func() string { return translation },
	})
	if err != nil {
		t.Fatalf("Refine failed: %v", err)
	}

	t.Logf("=== AFTER refine (Google) ===")
	t.Logf("Translation:\n%s", translation)
	t.Logf("Summary: proposed=%d applied=%d unresolved=%d", summary.TotalProposed, summary.TotalApplied, len(summary.Unresolved))
	t.Logf("Total edits proposed by model: %d", proposed)
	t.Logf("Total edits applied locally: %d", applied)
	if len(unresolved) > 0 {
		for _, u := range unresolved {
			t.Logf("  UNRESOLVED: original=%q replacement=%q", u.Original, u.Replacement)
		}
	}

	if proposed == 0 {
		t.Fatal("model did not propose any edits — tool-calling may not be working")
	}
	if applied == 0 && proposed > 0 {
		t.Errorf("model proposed %d edits but none applied (all not_found?)", proposed)
	}
	t.Logf("Refine completed: %d edits proposed, %d applied", proposed, applied)
}
