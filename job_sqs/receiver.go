package job_sqs

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type SqsReceiver struct {
	QueueName string
	SqsClient *sqs.SQS
}

func (receiver SqsReceiver) GetQueueURL(queue *string) (*sqs.GetQueueUrlOutput, error) {
	result, err := receiver.SqsClient.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: queue,
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (receiver SqsReceiver) ReceiveMsg() (*sqs.ReceiveMessageOutput, error) {
	urlResult, err := receiver.GetQueueURL(&receiver.QueueName)
	if err != nil {
		fmt.Println("Got an error getting the queue URL:")
		fmt.Println(err)
		return nil, err
	}

	queueURL := urlResult.QueueUrl
	//fmt.Println("Queue URL: ", *queueURL)
	fmt.Println("Polling job queue for new jobs...")

	var timeout int64
	timeout = 60
	msgResult, err1 := receiver.SqsClient.ReceiveMessage(&sqs.ReceiveMessageInput{
		AttributeNames: []*string{
			aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
		},
		MessageAttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
		QueueUrl:            queueURL,
		MaxNumberOfMessages: aws.Int64(5),
		VisibilityTimeout:   &timeout,
	})
	
	if err1 != nil {
		fmt.Println("Job queue Receive failed")
		return nil, err1
	}

	return msgResult, nil
}

func (receiver SqsReceiver) DeleteMsg(messageHandle *string) error {
	urlResult, err := receiver.GetQueueURL(&receiver.QueueName)
	if err != nil {
		fmt.Println("Got an error getting the queue URL:")
		fmt.Println(err)
		return err
	}

	queueURL := urlResult.QueueUrl
	_, err1 := receiver.SqsClient.DeleteMessage(&sqs.DeleteMessageInput{
		QueueUrl:      queueURL,
		ReceiptHandle: messageHandle,
	})
	
	if err1 != nil {
		fmt.Println("Failed to delete message")
		return err1
	}

	return nil
}

func (receiver SqsReceiver) CreateClient() *sqs.SQS {
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	client := sqs.New(sess)
	return client
}