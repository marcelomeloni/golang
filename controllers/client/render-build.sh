#!/usr/bin/env bash
set -o errexit

# Salva a pasta original (raiz do projeto) para não nos perdermos
ROOT_DIR=$PWD

# 1. Prepara a pasta de cache do Render para guardar o Chrome
STORAGE_DIR=/opt/render/project/.render
CHROME_DIR=$STORAGE_DIR/chrome

if [[ ! -d $CHROME_DIR/opt/google/chrome ]]; then
  echo "Baixando o Google Chrome..."
  mkdir -p $CHROME_DIR
  cd $CHROME_DIR
  
  wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
  dpkg -x ./google-chrome-stable_current_amd64.deb .
  rm ./google-chrome-stable_current_amd64.deb
  
  echo "Chrome baixado com sucesso."
else
  echo "Usando o Chrome salvo em cache."
fi

# 2. VOLTA para a pasta raiz do projeto antes de compilar
cd $ROOT_DIR

# 3. Compila a sua API Go (adicionei um ponto '.' no final por segurança)
echo "Compilando a API Go..."
go build -tags netgo -ldflags '-s -w' -o app .
