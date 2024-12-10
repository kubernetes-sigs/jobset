# Use the official Python image as the base image
FROM python:3.13

# Set the working directory inside the container
WORKDIR /app

# Copy the requirements.txt file to the container
COPY requirements.txt .
COPY test-requirements.txt .

# Install the Python packages specified in requirements.txt
RUN pip install --no-cache-dir -r test-requirements.txt -r requirements.txt

# Copy the rest of the application code to the container
COPY . .
