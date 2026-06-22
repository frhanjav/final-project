FROM python:3.12-alpine

WORKDIR /app

COPY task.py /app/task.py

CMD ["sleep", "infinity"]
