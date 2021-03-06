package flow

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/chrislusf/gleam/gio"
	"github.com/chrislusf/gleam/pb"
	"github.com/chrislusf/gleam/util"
)

// Output concurrently collects outputs from previous step to the driver.
func (d *Dataset) Output(f func(io.Reader) error) *Dataset {
	step := d.Flow.AddAllToOneStep(d, nil)
	step.IsOnDriverSide = true
	step.Name = "Output"
	step.Function = func(readers []io.Reader, writers []io.Writer, stat *pb.InstructionStat) error {
		errChan := make(chan error, len(readers))
		for i, reader := range readers {
			go func(i int, reader io.Reader) {
				errChan <- f(reader)
			}(i, reader)
		}
		for range readers {
			err := <-errChan
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to process output: %v\n", err)
				return err
			}
		}
		return nil
	}
	return d
}

// PipeOut writes to writer.
// If previous step is a Pipe() or PipeAsArgs(), the output is written as is.
// Otherwise, each row of output is written in tab-separated lines.
func (d *Dataset) PipeOut(writer io.Writer) *Dataset {
	fn := func(reader io.Reader) error {
		w := bufio.NewWriter(writer)
		defer w.Flush()
		if d.Step.IsPipe {
			_, err := io.Copy(w, reader)
			return err
		}
		return util.PrintDelimited(&pb.InstructionStat{}, reader, w, "\t", "\n")
	}
	return d.Output(fn)
}

// Fprintf formats using the format for each row and writes to writer.
func (d *Dataset) Fprintf(writer io.Writer, format string) *Dataset {
	fn := func(reader io.Reader) error {
		w := bufio.NewWriter(writer)
		defer w.Flush()
		if d.Step.IsPipe {
			return util.TsvPrintf(reader, w, format)
		}
		return util.Fprintf(reader, w, format)
	}
	return d.Output(fn)
}

// Fprintlnf add "\n" at the end of each format
func (d *Dataset) Fprintlnf(writer io.Writer, format string) *Dataset {
	return d.Fprintf(writer, format+"\n")
}

// Printf prints to os.Stdout in the specified format
func (d *Dataset) Printf(format string) *Dataset {
	return d.Fprintf(os.Stdout, format)
}

// Printlnf prints to os.Stdout in the specified format,
// adding an "\n" at the end of each format
func (d *Dataset) Printlnf(format string) *Dataset {
	return d.Fprintf(os.Stdout, format+"\n")
}

// SaveFirstRowTo saves the first row's values into the operands.
func (d *Dataset) SaveFirstRowTo(decodedObjects ...interface{}) *Dataset {
	fn := func(reader io.Reader) error {
		if d.Step.IsPipe {
			return util.TakeTsv(reader, 1, func(args []string) error {
				for i, o := range decodedObjects {
					if i >= len(args) {
						break
					}
					if v, ok := o.(*string); ok {
						*v = args[i]
					} else {
						return fmt.Errorf("Should save to *string.")
					}
				}
				return nil
			})
		}

		return util.TakeMessage(reader, 1, func(encodedBytes []byte) error {
			if row, err := util.DecodeRow(encodedBytes); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to decode byte: %v\n", err)
				return err
			} else {
				var counter int
				for _, v := range row.K {
					if err := setValueTo(v, decodedObjects[counter]); err != nil {
						return err
					}
					counter++
				}
				for _, v := range row.V {
					if err := setValueTo(v, decodedObjects[counter]); err != nil {
						return err
					}
					counter++
				}
			}
			return nil
		})
	}
	return d.Output(fn)
}

func (d *Dataset) OutputRow(f func(*util.Row) error) *Dataset {
	fn := func(reader io.Reader) error {
		if d.Step.IsPipe {
			return util.TakeTsv(reader, -1, func(args []string) error {
				var objects []interface{}
				for _, arg := range args {
					objects = append(objects, arg)
				}
				row := util.NewRow(util.Now(), objects...)
				return f(row)
			})
		}

		return util.TakeMessage(reader, -1, func(encodedBytes []byte) error {
			if row, err := util.DecodeRow(encodedBytes); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to decode byte: %v\n", err)
				return err
			} else {
				return f(row)
			}
			return nil
		})
	}
	return d.Output(fn)
}

func setValueTo(src, dst interface{}) error {
	switch v := dst.(type) {
	case *string:
		*v = gio.ToString(src)
	case *[]byte:
		*v = src.([]byte)
	case *int:
		*v = int(gio.ToInt64(src))
	case *int8:
		*v = int8(gio.ToInt64(src))
	case *int16:
		*v = int16(gio.ToInt64(src))
	case *int32:
		*v = int32(gio.ToInt64(src))
	case *int64:
		*v = gio.ToInt64(src)
	case *uint:
		*v = uint(gio.ToInt64(src))
	case *uint8:
		*v = uint8(gio.ToInt64(src))
	case *uint16:
		*v = uint16(gio.ToInt64(src))
	case *uint32:
		*v = uint32(gio.ToInt64(src))
	case *uint64:
		*v = uint64(gio.ToInt64(src))
	case *bool:
		*v = src.(bool)
	case *float32:
		*v = float32(gio.ToFloat64(src))
	case *float64:
		*v = gio.ToFloat64(src)
	}

	v := reflect.ValueOf(dst)
	if !v.IsValid() {
		return errors.New("setValueTo nil")
	}
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("setValueTo to nonsettable %T", dst)
	}
	return nil
}
