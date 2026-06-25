# MAX Robux Gift Card Bot

Go-сервер для MAX-бота, который продает Roblox Gift Cards через Kinguin и выдает код после успешной оплаты YooKassa.

## Что умеет

- Стартовое меню в MAX.
- Кнопки номиналов: 400, 800, 2000, 4500 Robux.
- Проверка наличия и цены товара в Kinguin.
- Расчет цены в рублях с курсом и наценкой.
- Создание платежной ссылки YooKassa.
- Webhook успешной оплаты.
- Автоматический выкуп кода в Kinguin.
- Отправка кода пользователю в MAX.
- Статистика заказов для админов через `/stats`.

## Настройка

Скопируйте пример env:

```bash
cp .env.example .env
```

Заполните:

- `MAX_BOT_TOKEN` - токен бота MAX.
- `MAX_WEBHOOK_SECRET` - секрет webhook.
- `PUBLIC_BASE_URL` - публичный HTTPS-адрес сервера.
- `KINGUIN_API_KEY` - API-ключ Kinguin.
- `PRODUCT_400_ROBUX`, `PRODUCT_800_ROBUX`, `PRODUCT_2000_ROBUX`, `PRODUCT_4500_ROBUX` - ID товаров Kinguin.
- `YOOKASSA_SHOP_ID`, `YOOKASSA_SECRET_KEY` - платежные доступы.
- `ADMIN_PLATFORM_IDS` - ID админов через запятую.

Токены не нужно хранить в git. Если токен MAX уже был отправлен в переписке, лучше перевыпустить его в кабинете.

## Запуск

```bash
docker compose up -d --build
```

Локально:

```bash
go run ./cmd/bot
```

## Webhook

При запуске сервер сам пытается подписать MAX webhook:

```text
POST {PUBLIC_BASE_URL}/webhook/max
```

YooKassa webhook:

```text
POST {PUBLIC_BASE_URL}/pay/yookassa/webhook
```

Return URL для платежей:

```text
GET {PUBLIC_BASE_URL}/pay/success?order={id}
```

## Статусы заказов

- `created` - заказ создан.
- `pending` - платежная ссылка создана.
- `paid` - платеж подтвержден.
- `success` - код куплен и выдан.
- `error` - ошибка до оплаты или при создании платежа.
- `manual` - деньги списаны, но код требует ручной выдачи.

## Важные замечания

Точные поля ответа Kinguin могут отличаться от аккаунта/API-версии. Клиент старается найти `price`, `qty`, `code` в нескольких распространенных местах, но после подключения реального Kinguin-ключа нужно сделать тестовую покупку на минимальном номинале.
