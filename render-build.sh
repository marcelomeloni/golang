#!/usr/bin/env bash
# Interrompe o script se der erro
set -o errexit

echo "Compilando a API Go..."
go build -o api .

# Define onde o Chrome vai ficar salvo no Render
STORAGE_DIR=/opt/render/project/.render
CHROME_DIR=$STORAGE_DIR/chrome

if [[ ! -d $CHROME_DIR ]]; then
  echo "Baixando o Google Chrome..."
  mkdir -p $CHROME_DIR
  cd $CHROME_DIR
  
  # Baixa a versão estável do Chrome para Linux
  wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
  
  # Extrai o conteúdo do pacote .deb sem precisar de permissão root
  dpkg -x ./google-chrome-stable_current_amd64.deb .
  rm ./google-chrome-stable_current_amd64.deb
  
  echo "Chrome baixado com sucesso."
else
  echo "Usando o Chrome salvo em cache."
fi
