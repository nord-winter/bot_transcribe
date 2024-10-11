package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	speech "cloud.google.com/go/speech/apiv1"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"google.golang.org/api/option"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

var selectedModel map[int64]string // Хранит выбор модели для каждого пользователя

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

	selectedModel = make(map[int64]string) // Инициализация карты для хранения выбранных моделей

	for update := range updates {
		if update.CallbackQuery != nil {
			handleCallbackQuery(bot, update.CallbackQuery)
		} else if update.Message != nil {
			if update.Message.Audio != nil {
				handleAudioMessage(bot, update.Message)
			} else {
				switch update.Message.Command() {
				case "start":
					sendModelSelection(bot, update.Message.Chat.ID)
				case "help":
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Hi! I'm an audio transcription bot. Send me an audio file and I'll transcribe it for you.")
					bot.Send(msg)
				}
			}
		}
	}
}

// Функция для отображения кнопок выбора модели
func sendModelSelection(bot *tgbotapi.BotAPI, chatID int64) {
	modelSelectionKeyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Whisper", "model_whisper"),
			tgbotapi.NewInlineKeyboardButtonData("Google Speech-to-Text", "model_google"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, "Выберите модель для распознавания:")
	msg.ReplyMarkup = modelSelectionKeyboard
	bot.Send(msg)
}

// Обработка нажатий на inline-кнопки
func handleCallbackQuery(bot *tgbotapi.BotAPI, callbackQuery *tgbotapi.CallbackQuery) {
	switch callbackQuery.Data {
	case "model_whisper":
		// Whisper выбран, сохраняем выбор и просим загрузить аудиофайл
		selectedModel[callbackQuery.Message.Chat.ID] = "whisper"
		msg := tgbotapi.NewMessage(callbackQuery.Message.Chat.ID, "Вы выбрали Whisper. Пожалуйста, загрузите аудиофайл.")
		bot.Send(msg)
	case "model_google":
		// Google Speech-to-Text выбран, сохраняем выбор и просим загрузить аудиофайл
		selectedModel[callbackQuery.Message.Chat.ID] = "google"
		msg := tgbotapi.NewMessage(callbackQuery.Message.Chat.ID, "Вы выбрали Google Speech-to-Text. Пожалуйста, загрузите аудиофайл.")
		bot.Send(msg)
	}

	// Подтверждаем получение callback'а
	callback := tgbotapi.NewCallback(callbackQuery.ID, "Модель выбрана")
	bot.Send(callback)
}

// Обработка аудиофайла
func handleAudioMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	chatID := message.Chat.ID
	audio := message.Audio

	// Скачиваем файл с аудиосообщением
	fileURL, _ := bot.GetFileDirectURL(audio.FileID)
	localFilePath := downloadFile(fileURL, audio.FileID+".ogg")

	// Конвертируем ogg в wav
	wavFilePath := convertOggToWav(localFilePath)

	// Проверяем, какую модель выбрал пользователь
	model := selectedModel[chatID]
	switch model {
	case "whisper":
		// Транскрипция через Whisper
		transcribedText := transcribeWithWhisper(wavFilePath)
		sendTranscriptionAsFile(bot, chatID, transcribedText)
	case "google":
		// Транскрипция через Google Speech-to-Text
		transcribedText, err := transcribeWithGoogleSpeechToText(wavFilePath)
		if err != nil {
			log.Printf("Error during Google Speech-to-Text transcription: %v", err)
			return
		}
		sendTranscriptionAsFile(bot, chatID, transcribedText)
	}
}

// Функция для скачивания файла
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

// Функция для конвертации OGG в WAV
func convertOggToWav(inputFile string) string {
	outputFile := strings.TrimSuffix(inputFile, filepath.Ext(inputFile)) + ".wav"

	cmd := exec.Command("ffmpeg", "-i", inputFile, "-ar", "16000", "-ac", "1", outputFile)
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to convert file: %v", err)
	}
	return outputFile
}

// Функция для транскрипции через Whisper
func transcribeWithWhisper(audioFilePath string) string {
	cmd := exec.Command("./whisper.cpp/main", "-m", "whisper.cpp/models/ggml-large-v3-turbo.bin", "-f", audioFilePath, "-l", "th")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to transcribe with Whisper: %v", err)
	}

	return string(output)
}

// Функция для транскрипции через Google Speech-to-Text
func transcribeWithGoogleSpeechToText(audioFilePath string) (string, error) {
	// Настройка аутентификации с использованием ключей сервисного аккаунта
	ctx := context.Background()
	client, err := speech.NewClient(ctx, option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")))
	if err != nil {
		return "", err
	}
	defer client.Close()

	// Открываем аудиофайл
	audioFile, err := os.Open(audioFilePath)
	if err != nil {
		return "", err
	}
	defer audioFile.Close()

	// Чтение аудиофайла
	audioData, err := io.ReadAll(audioFile)
	if err != nil {
		return "", err
	}

	// Настройка запроса к Google Speech-to-Text API
	req := &speechpb.RecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:        speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz: 16000,
			LanguageCode:    "th-TH",
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Content{Content: audioData},
		},
	}

	// Отправка запроса и получение результата
	resp, err := client.Recognize(ctx, req)
	if err != nil {
		return "", err
	}

	// Получение транскрибированного текста
	var resultText strings.Builder
	for _, result := range resp.Results {
		for _, alt := range result.Alternatives {
			resultText.WriteString(alt.Transcript)
			resultText.WriteString("\n")
		}
	}

	return resultText.String(), nil
}

// Функция для отправки файла с транскрипцией
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
