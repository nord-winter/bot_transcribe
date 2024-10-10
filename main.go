package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Panic("TELEGRAM_BOT_TOKEN is not set")
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			// Проверяем, является ли сообщение пересланным аудиофайлом
			if update.Message.Audio != nil {
				audio := update.Message.Audio

				// Скачиваем файл с аудиосообщением
				fileURL, _ := bot.GetFileDirectURL(audio.FileID)
				localFilePath := downloadFile(fileURL, audio.FileID+".ogg")

				// Конвертируем ogg в wav
				wavFilePath := convertOggToWav(localFilePath)

				// Транскрипция через Whisper
				transcribedText := transcribeWithWhisper(wavFilePath)

				// // Отправляем текст пользователю
				// bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, transcribedText))
				err := sendTranscriptionAsFile(bot, update.Message.Chat.ID, transcribedText)
				if err != nil {
					log.Fatalf("Failed to send transcription as file: %v", err)
				}
			}
		}

	}
}

func sendTranscriptionAsFile(bot *tgbotapi.BotAPI, chatID int64, transcription string) error {
	// Сохраняем транскрибированный текст в файл
	fileName := "transcription.txt"
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Записываем текст в файл
	_, err = file.WriteString(transcription)
	if err != nil {
		return err
	}

	// Открываем файл для отправки
	fileToSend, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer fileToSend.Close()

	// Отправляем файл пользователю
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   fileName,
		Reader: fileToSend,
	})

	_, err = bot.Send(doc)
	if err != nil {
		return err
	}

	return nil
}

func downloadFile(fileURL, filePath string) string {
	out, err := os.Create(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	resp, err := http.Get(fileURL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	return filePath
}

func convertOggToWav(inputFile string) string {

	outputFile := strings.TrimSuffix(inputFile, filepath.Ext(inputFile)) + ".wav"

	cmd := exec.Command("ffmpeg", "-i", inputFile, "-ar", "16000", "-ac", "1", outputFile)
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to convert file: %v", err)
	}
	return outputFile
}

func transcribeWithWhisper(audioFilePath string) string {
	cmd := exec.Command("./whisper.cpp/main", "-m", "whisper.cpp/models/ggml-large-v3-turbo.bin", "-f", audioFilePath, "-l", "th")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to transcribe with Whisper: %v", err)
	}

	return string(output)
}
