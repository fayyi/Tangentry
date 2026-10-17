import { articleTranslationParts } from './articleTranslation'

const segmenter = new Intl.Segmenter('en', { granularity: 'sentence' })

export function descriptionTranslationParts(text: string) {
  const sentences = text.split(/(\n+)/).flatMap(line => {
    if (!line.trim()) return line ? [line] : []
    const parts: string[] = []
    for (const { segment } of segmenter.segment(line)) {
      // Sentence segmentation can separate a person's title from their name.
      if (parts.length && /\b(?:Mr|Mrs|Ms|Dr|Prof|Sr|Jr|St)\.\s*$/i.test(parts[parts.length - 1])) parts[parts.length - 1] += segment
      else parts.push(segment)
    }
    return parts
  })
  return { sentences, ...articleTranslationParts(sentences.map(sentence => sentence.trim() ? sentence : null)) }
}
