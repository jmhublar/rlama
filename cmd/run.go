package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/dontizi/rlama/internal/domain"
	"github.com/dontizi/rlama/internal/service"
	"github.com/spf13/cobra"
)

var (
	contextSize      int
	promptTemplate   string
	queryTemplate    string
	printChunks      bool
	streamOutput     bool
	apiProfileName   string
	maxTokens        int
	temperature      float64
	autoRetrievalAPI bool
	useGUI           bool
	showContext      bool
)

var runCmd = &cobra.Command{
	Use:   "run [rag-name]",
	Short: "Run a RAG system",
	Long: `Run a previously created RAG system. 
Starts an interactive session to interact with the RAG system.
Example: rlama run rag1

If the --prompt or --query flag is provided with a value, the command will run non-interactively,
process the given prompt, print the answer, and then exit.
Examples: 
  rlama run rag1 --prompt "What is RLAMA?"
  rlama run rag1 --query "How does RAG work?"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ragName := args[0]

		ollamaClient := GetOllamaClient()
		if err := ollamaClient.CheckOllamaAndModel(""); err != nil {
			return err
		}

		ragService := service.NewRagService(ollamaClient)
		rag, err := ragService.LoadRag(ragName)
		if err != nil {
			return err
		}

		fmt.Printf("RAG '%s' loaded. Model: %s\n", rag.Name, rag.ModelName)

		// Check if a non-interactive prompt is provided (--query takes priority over --prompt)
		questionFromFlag := strings.TrimSpace(queryTemplate)
		if questionFromFlag == "" {
			questionFromFlag = strings.TrimSpace(promptTemplate) // Fallback to --prompt if --query not provided
		}

		if questionFromFlag != "" {
			// Non-interactive mode
			fmt.Printf("Processing non-interactive prompt: \"%s\"\n", questionFromFlag)

			fmt.Println("\nSearching documents for relevant information...")
			checkWatchedResources(rag, ragService)

			if showContext {
				embeddingService := service.NewEmbeddingService(ollamaClient)
				queryEmbedding, errEmb := embeddingService.GenerateQueryEmbedding(questionFromFlag, rag.ModelName)
				if errEmb != nil {
					fmt.Printf("Error generating embedding: %s\n", errEmb)
				} else {
					results := rag.HybridStore.Search(queryEmbedding, contextSize)
					fmt.Printf("\n--- Debug: Retrieved %d chunks ---\n", len(results))
					for i, result := range results {
						chunk := rag.GetChunkByID(result.ID)
						if chunk != nil {
							fmt.Printf("%d. [Score: %.4f] %s\n", i+1, result.Score, chunk.GetMetadataString())
							if i < 3 { // Show content for top 3 chunks
								fmt.Printf("   Preview: %s\n", truncateString(chunk.Content, 100))
							}
						}
					}
					fmt.Println("--- End Debug ---")
				}
			}

			answer, errQuery := ragService.Query(rag, questionFromFlag, contextSize)
			if errQuery != nil {
				// For non-interactive, return the error to indicate failure
				return fmt.Errorf("error querying RAG: %w", errQuery)
			}

			fmt.Println("\n--- Answer ---")
			fmt.Println(answer)
			fmt.Println()
			return nil // Exit after processing the single prompt
		}

		// Interactive mode (original behavior)
		if showContext {
			fmt.Printf("Debug info: RAG contains %d documents and %d total chunks\n",
				len(rag.Documents), len(rag.Chunks))
			fmt.Printf("Chunking strategy: %s, Size: %d, Overlap: %d\n",
				rag.ChunkingStrategy,
				rag.WatchOptions.ChunkSize,
				rag.WatchOptions.ChunkOverlap)
			if rag.RerankerEnabled {
				fmt.Printf("Reranking: Enabled (weight: %.2f)\n",
					rag.RerankerWeight)
				defaultOpts := service.DefaultRerankerOptions()
				if contextSize <= 0 {
					fmt.Printf("Using default TopK: %d\n", defaultOpts.TopK)
				} else {
					fmt.Printf("Using custom TopK: %d\n", contextSize)
				}
			} else {
				fmt.Println("Reranking: Disabled")
			}
		}
		fmt.Println("Type your question (or 'exit' to quit):")

		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("> ")
			if !scanner.Scan() {
				break
			}

			question := scanner.Text()
			if question == "exit" {
				break
			}

			if strings.TrimSpace(question) == "" {
				continue
			}

			fmt.Println("\nSearching documents for relevant information...")

			checkWatchedResources(rag, ragService)

			// If debug mode is enabled, get the chunks manually first
			if showContext {
				// Call embeddingService directly through ragService to generate embedding
				embeddingService := service.NewEmbeddingService(ollamaClient)
				queryEmbedding, err := embeddingService.GenerateQueryEmbedding(question, rag.ModelName)
				if err != nil {
					fmt.Printf("Error generating embedding: %s\n", err)
				} else {
					results := rag.HybridStore.Search(queryEmbedding, contextSize)

					// Show detailed results
					fmt.Printf("\n--- Debug: Retrieved %d chunks ---\n", len(results))
					for i, result := range results {
						chunk := rag.GetChunkByID(result.ID)
						if chunk != nil {
							fmt.Printf("%d. [Score: %.4f] %s\n", i+1, result.Score, chunk.GetMetadataString())
							if i < 3 { // Show content for top 3 chunks only to avoid overload
								fmt.Printf("   Preview: %s\n", truncateString(chunk.Content, 100))
							}
						}
					}
					fmt.Println("--- End Debug ---")
				}
			}

			answer, err := ragService.Query(rag, question, contextSize)
			if err != nil {
				fmt.Printf("Error: %s\n", err)
				continue
			}

			fmt.Println("\n--- Answer ---")
			fmt.Println(answer)
			fmt.Println()
		}

		return nil
	},
}

// Helper function to truncate string for preview
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Add flags
	runCmd.Flags().IntVar(&contextSize, "context-size", 0, "Number of chunks to use as context (0 = auto: 5 with reranking, 20 without)")
	runCmd.Flags().StringVar(&promptTemplate, "prompt", "", "Custom prompt template to use (enclose in quotes)")
	runCmd.Flags().StringVarP(&queryTemplate, "query", "q", "", "Query to execute non-interactively (enclose in quotes)")
	runCmd.Flags().BoolVar(&printChunks, "print-chunks", false, "Print the chunks used for the response")
	runCmd.Flags().BoolVar(&streamOutput, "stream", true, "Stream the model's output")
	runCmd.Flags().StringVar(&apiProfileName, "profile", "", "API profile name for OpenAI models")
	runCmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Maximum number of tokens to generate (0 = model's default)")
	runCmd.Flags().Float64Var(&temperature, "temperature", 0.7, "Temperature for sampling (higher = more random)")
	runCmd.Flags().BoolVar(&autoRetrievalAPI, "auto-retrieval", false, "Use model's built-in retrieval API if available")
	runCmd.Flags().BoolVarP(&useGUI, "gui", "g", false, "Use GUI mode")
	runCmd.Flags().BoolVar(&showContext, "show-context", false, "Show retrieved chunks and context information")
}

func checkWatchedResources(rag *domain.RagSystem, ragService service.RagService) {
	// Check watched directory if enabled with on-use check
	if rag.WatchEnabled && rag.WatchInterval == 0 {
		fileWatcher := service.NewFileWatcher(ragService)
		docsAdded, err := fileWatcher.CheckAndUpdateRag(rag)
		if err != nil {
			fmt.Printf("Error checking watched directory: %v\n", err)
		} else if docsAdded > 0 {
			fmt.Printf("Added %d new documents from watched directory.\n", docsAdded)
		}
	}

	// Check watched website if enabled with on-use check
	if rag.WebWatchEnabled && rag.WebWatchInterval == 0 {
		webWatcher := service.NewWebWatcher(ragService)
		pagesAdded, err := webWatcher.CheckAndUpdateRag(rag)
		if err != nil {
			fmt.Printf("Error checking watched website: %v\n", err)
		} else if pagesAdded > 0 {
			fmt.Printf("Added %d new pages from watched website.\n", pagesAdded)
		}
	}
}
