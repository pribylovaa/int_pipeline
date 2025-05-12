package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// logAction - вспомогательная функция для логирования действий
func logAction(action string) {
	fmt.Printf("[]LOG %s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), action)
}

// bufferDrainInterval - интервал очистки кольцевого буфера
const bufferDrainInterval time.Duration = 15 * time.Second

// bufferSize - размер кольцевого буфера
const bufferSize int = 10

// RingBuffer - кольцевой буфер целых чисел
type RingBuffer struct {
	data     []int
	head     int
	tail     int
	size     int // количество элементов в буфере
	capacity int // максимальная емкость
	m        sync.Mutex
}

// NewRingBuffer - создание нового кольцевого буфера с заданной емкостью
func NewRingBuffer(capacity int) *RingBuffer {
	logAction(fmt.Sprintf("Создание нового кольцевого буфера с емкостью = %d", capacity))
	return &RingBuffer{
		data:     make([]int, capacity),
		head:     0,
		tail:     0,
		size:     0,
		capacity: capacity,
		m:        sync.Mutex{},
	}
}

// Push - добавление элемента в конец буфера
func (r *RingBuffer) Push(value int) {
	r.m.Lock()
	defer r.m.Unlock()

	logAction(fmt.Sprintf("RingBuffer.Push: добавление значения %d", value))
	r.data[r.tail] = value
	r.tail = (r.tail + 1) % r.capacity

	if r.size == r.capacity {
		r.head = (r.head + 1) % r.capacity
		logAction(fmt.Sprintf("RingBuffer.Push: буфер полон, head перемещен на %d", r.head))
	} else {
		r.size++
	}
	logAction(fmt.Sprintf("RingBuffer.Push: текущее состояние - size=%d, head=%d, tail=%d", r.size, r.head, r.tail))
}

// Get - получение данных с последующей очисткой
func (r *RingBuffer) Get() []int {
	r.m.Lock()
	defer r.m.Unlock()

	if r.size == 0 {
		logAction("RingBuffer.Get: буфер пуст, возвращаем пустой срез")
		return []int{}
	}

	output := make([]int, r.size)
	if r.head < r.tail {
		copy(output, r.data[r.head:r.tail])
		logAction(fmt.Sprintf("RingBuffer.Get: копирование данных из непрерывного диапазона [%d:%d]", r.head, r.tail))
	} else {
		copy(output, r.data[r.head:])
		copy(output[r.capacity-r.head:], r.data[:r.tail])
		logAction(fmt.Sprintf("RingBuffer.Get: копирование данных из двух диапазонов [%d:] и [:%d]", r.head, r.tail))
	}

	// очистка буфера
	for i := 0; i < r.capacity; i++ {
		r.data[i] = 0
	}
	r.head = 0
	r.tail = 0
	r.size = 0

	logAction(fmt.Sprintf("RingBuffer.Get: буфер очищен, возвращено %d элементов", len(output)))

	return output
}

// Stage - стадия пайплайна, обрабатывающая целые числа
type Stage func(context.Context, <-chan int) <-chan int

// Pipeline - пайплайн обработки целых чисел
type Pipeline struct {
	ctx    context.Context
	stages []Stage
}

// NewPipeline - инициализация пайплайна
func NewPipeline(ctx context.Context, stages ...Stage) *Pipeline {
	logAction("Создание нового пайплайна")
	return &Pipeline{ctx: ctx, stages: stages}
}

// RunStage - запуск отдельной стадии пайплайна
func (p *Pipeline) RunStage(inputChan <-chan int, stage Stage) <-chan int {
	logAction("Запуск новой стадии пайплайна")
	return stage(p.ctx, inputChan)
}

// Run - запуск пайплайна
func (p *Pipeline) Run() <-chan int {
	logAction("Pipeline.Run: запуск пайплайна")
	if len(p.stages) == 0 {
		outputChan := make(chan int)
		close(outputChan)
		logAction("Pipeline.Run: нет стадий для выполнения, возвращаем закрытый канал")
		return outputChan
	}

	var outputChan <-chan int = p.stages[0](p.ctx, nil)
	logAction("Pipeline.Run: запущена первая стадия")

	for i := 1; i < len(p.stages); i++ {
		logAction(fmt.Sprintf("Pipeline.Run: запуск стадии %d", i+1))
		outputChan = p.RunStage(outputChan, p.stages[i])
	}

	logAction("Pipeline.Run: все стадии запущены")
	return outputChan
}

// SourceStage - стадия источника данных (чтение из консоли)
func SourceStage(ctx context.Context, _ <-chan int) <-chan int {
	outputChan := make(chan int)
	logAction("SourceStage: инициализация")

	go func() {
		defer close(outputChan)
		// создаем новый сканер, который будет построчно
		// считывать ввод пользователя с консоли
		scanner := bufio.NewScanner(os.Stdin)
		for {
			select {
			case <-ctx.Done():
				logAction("SourceStage: получен сигнал завершения контекста")
				return
			default:
				fmt.Println("Введите число (или 'exit' для выхода):")
				if !scanner.Scan() {
					if err := scanner.Err(); err != nil {
						logAction(fmt.Sprintf("SourceStage: ошибка при чтении ввода: %v", err))
						fmt.Fprintf(os.Stderr, "Ошибка при чтении ввода: %v\n", err)
					}
					logAction("SourceStage: сканер завершил работу")
					return
				}
				data := scanner.Text()

				// проверка на команду выхода
				if strings.EqualFold(data, "exit") {
					logAction("SourceStage: получена команда 'exit'")
					fmt.Println("Программа завершает работу!")
					return
				}

				// преобразование строки в число
				i, err := strconv.Atoi(data)
				if err != nil {
					logAction(fmt.Sprintf("SourceStage: ошибка преобразования '%s' в число: %v", data, err))
					fmt.Println("Программа обрабатывает только целые числа!")
					continue
				}

				logAction(fmt.Sprintf("SourceStage: отправка числа %d в канал", i))
				// отправка данных в канал или обработка сигнала завершения
				select {
				case outputChan <- i:
				case <-ctx.Done():
					logAction("SourceStage: получен сигнал завершения во время отправки данных")
					return
				}
			}
		}
	}()
	return outputChan
}

// NegativeFilterStage - стадия фильтрации отрицательных чисел
func NegativeFilterStage(ctx context.Context, intputChan <-chan int) <-chan int {
	outputChan := make(chan int)
	logAction("NegativeFilterStage: инициализация")

	go func() {
		defer close(outputChan)
		for {
			select {
			case data, ok := <-intputChan:
				if !ok {
					logAction("NegativeFilterStage: входной канал закрыт")
					fmt.Println("Канал закрыт, завершаем работу NegativeFilterStage.")
					return
				}
				logAction(fmt.Sprintf("NegativeFilterStage: получено число %d", data))
				if data >= 0 {
					logAction(fmt.Sprintf("NegativeFilterStage: число %d прошло фильтрацию", data))
					select {
					case outputChan <- data:
					case <-ctx.Done():
						logAction("NegativeFilterStage: получен сигнал завершения во время отправки данных")
						return
					}
				} else {
					logAction(fmt.Sprintf("NegativeFilterStage: число %d отфильтровано (отрицательное)", data))
				}
			case <-ctx.Done():
				logAction("NegativeFilterStage: получен сигнал завершения контекста")
				return
			}
		}
	}()
	return outputChan
}

// SpecialFilterStage - стадия фильтрации чисел, не кратных трем
func SpecialFilterStage(ctx context.Context, inputChan <-chan int) <-chan int {
	outputChan := make(chan int)
	logAction("SpecialFilterStage: инициализация")

	go func() {
		defer close(outputChan)
		for {
			select {
			case data, ok := <-inputChan:
				if !ok {
					logAction("SpecialFilterStage: входной канал закрыт")
					fmt.Println("Канал закрыт, завершаем работу SpecialFilterStage.")
					return
				}
				logAction(fmt.Sprintf("SpecialFilterStage: получено число %d", data))
				if data != 0 && data%3 == 0 {
					logAction(fmt.Sprintf("SpecialFilterStage: число %d прошло фильтрацию (кратно 3)", data))
					select {
					case outputChan <- data:
					case <-ctx.Done():
						logAction("SpecialFilterStage: получен сигнал завершения во время отправки данных")
						return
					}
				} else {
					logAction(fmt.Sprintf("SpecialFilterStage: число %d отфильтровано (не кратно 3)", data))
				}
			case <-ctx.Done():
				logAction("SpecialFilterStage: получен сигнал завершения контекста")
				return
			}
		}
	}()
	return outputChan
}

// BufferStage - стадия буферизации
func BufferStage(ctx context.Context, inputChan <-chan int) <-chan int {
	outputChan := make(chan int)
	buffer := NewRingBuffer(bufferSize)
	logAction("BufferStage: инициализация")

	go func() {
		defer close(outputChan)
		for {
			select {
			case data, ok := <-inputChan:
				if !ok {
					logAction("BufferStage: входной канал закрыт, сброс буфера")
					fmt.Println("Канал закрыт, завершаем работу BufferStage.")
					bufferData := buffer.Get()
					for _, val := range bufferData {
						select {
						case outputChan <- val:
							logAction(fmt.Sprintf("BufferStage: отправка буферизированного значения %d", val))
						case <-ctx.Done():
							logAction("BufferStage: получен сигнал завершения во время сброса буфера")
							return
						}
					}
					return
				}
				logAction(fmt.Sprintf("BufferStage: получено число %d, добавление в буфер", data))
				buffer.Push(data)
			case <-time.After(bufferDrainInterval):
				logAction("BufferStage: сработал таймер очистки буфера")
				bufferData := buffer.Get()
				if len(bufferData) > 0 {
					logAction(fmt.Sprintf("BufferStage: сброс %d элементов из буфера", len(bufferData)))
					for _, data := range bufferData {
						select {
						case outputChan <- data:
						case <-ctx.Done():
							logAction("BufferStage: получен сигнал завершения во время сброса буфера по таймеру")
							return
						}
					}
				} else {
					logAction("BufferStage: буфер пуст, сбрасывать нечего")
				}
			case <-ctx.Done():
				logAction("BufferStage: получен сигнал завершения контекста")
				return
			}
		}
	}()
	return outputChan
}

// ConsumerStage - стадия потребителя данных (вывод в консоль)
func ConsumerStage(ctx context.Context, inputChan <-chan int) <-chan int {
	outputChan := make(chan int)
	logAction("ConsumerStage: инициализация")

	go func() {
		defer close(outputChan)
		for {
			select {
			case data, ok := <-inputChan:
				if !ok {
					logAction("ConsumerStage: входной канал закрыт")
					fmt.Println("Канал закрыт, завершаем работу ConsumerStage.")
					return
				}
				logAction(fmt.Sprintf("ConsumerStage: вывод числа %d", data))
				fmt.Printf("Получены данные: %d\n", data)
			case <-ctx.Done():
				logAction("ConsumerStage: получен сигнал завершения контекста")
				return
			}
		}
	}()
	return outputChan
}

func main() {
	logAction("Запуск программы")
	// создаем контекст с возможностью отмены
	ctx, canсel := context.WithCancel(context.Background())
	defer canсel()

	// Создаем пайплайн
	pipeline := NewPipeline(
		ctx,
		SourceStage,
		NegativeFilterStage,
		SpecialFilterStage,
		BufferStage,
		ConsumerStage,
	)

	// Запускаем пайплайн
	outputChan := pipeline.Run()

	logAction("Чтение из выходного канала пайплайна")
	// Читаем из конечного канала, чтобы пайплайн работал
	for range outputChan {
	}
	logAction("Программа завершена")
}
